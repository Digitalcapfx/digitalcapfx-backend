package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	mrand "math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/pquerna/otp/totp"

	"github.com/rachfinance/digitalfx/internal/clients/google"
	"github.com/rachfinance/digitalfx/internal/config"
	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
	"github.com/rachfinance/digitalfx/internal/pkg/email"
	"github.com/rachfinance/digitalfx/internal/pkg/hash"
	"github.com/rachfinance/digitalfx/internal/pkg/token"
)

var (
	ErrUserNotFound    = errors.New("user not found")
	ErrUserExists      = errors.New("user already exists")
	ErrInvalidPIN      = errors.New("invalid pin")
	ErrInvalidOTP      = errors.New("invalid or expired otp")
	ErrAccountInactive = errors.New("account is inactive")
	ErrSessionNotFound = errors.New("session not found")
	ErrInvalidToken    = errors.New("invalid or expired token")
	ErrSocialAuthUser  = errors.New("account uses social login — PIN not set")
)

// LoginResult is returned by Login. When Requires2FA is true the client must
// call CompleteTOTPLogin with TOTPRef + a valid TOTP code to get a token pair.
type LoginResult struct {
	Pair        *token.Pair
	Requires2FA bool
	TOTPRef     string
}

type AuthService struct {
	pool        *pgxpool.Pool
	rdb         *redis.Client
	cfg         *config.Config
	logger      *zap.Logger
	emailClient *email.Client
}

func NewAuthService(pool *pgxpool.Pool, rdb *redis.Client, cfg *config.Config, logger *zap.Logger, emailClient *email.Client) *AuthService {
	return &AuthService{pool: pool, rdb: rdb, cfg: cfg, logger: logger, emailClient: emailClient}
}

// ─── Phone OTP ────────────────────────────────────────────────────────────────

func (s *AuthService) SendOTP(ctx context.Context, phone string) error {
	q := db.New(s.pool)

	code := fmt.Sprintf("%06d", mrand.Intn(1000000))
	expires := time.Now().Add(10 * time.Minute)

	if _, err := q.CreateOTP(ctx, db.CreateOTPParams{
		PhoneNumber: phone,
		Code:        code,
		ExpiresAt:   expires,
	}); err != nil {
		return fmt.Errorf("create otp: %w", err)
	}

	// TODO: deliver via SMS provider (Twilio / Termii / Africa's Talking)
	s.logger.Info("phone OTP created", zap.String("phone", phone), zap.String("code", code))
	return nil
}

func (s *AuthService) VerifyOTP(ctx context.Context, phone, code string) error {
	q := db.New(s.pool)

	otp, err := q.GetValidOTP(ctx, db.GetValidOTPParams{
		PhoneNumber: phone,
		Code:        code,
	})
	if err != nil {
		return ErrInvalidOTP
	}

	return q.MarkOTPUsed(ctx, otp.ID)
}

// ─── Register ─────────────────────────────────────────────────────────────────

type RegisterInput struct {
	Phone     string
	Email     string
	FirstName string
	LastName  string
	PIN       string
	DeviceIP  string
	DeviceUA  string
}

func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*token.Pair, error) {
	q := db.New(s.pool)

	if _, err := q.GetUserByPhone(ctx, in.Phone); err == nil {
		return nil, ErrUserExists
	}

	pinHash, err := hash.PIN(in.PIN)
	if err != nil {
		return nil, fmt.Errorf("hash pin: %w", err)
	}

	var emailPtr *string
	if in.Email != "" {
		emailPtr = &in.Email
	}

	user, err := q.CreateUser(ctx, db.CreateUserParams{
		PhoneNumber:  in.Phone,
		Email:        emailPtr,
		FirstName:    in.FirstName,
		LastName:     in.LastName,
		PinHash:      &pinHash,
		Role:         "user",
		AuthProvider: "phone",
	})
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	// Provision default fiat accounts.
	for _, currency := range []string{"XAF", "XOF", "USD", "GBP", "EUR"} {
		if _, err := q.CreateAccount(ctx, db.CreateAccountParams{
			UserID:        user.ID,
			Currency:      currency,
			AccountNumber: generateAccountNumber(),
		}); err != nil {
			s.logger.Error("create account", zap.String("currency", currency), zap.Error(err))
		}
	}

	pair, err := s.createSession(ctx, q, user.ID, user.PhoneNumber, user.Role, in.DeviceIP, in.DeviceUA)
	if err != nil {
		return nil, err
	}

	if user.Email != nil {
		go s.sendWelcomeEmail(*user.Email, user.FirstName)
		go func() {
			if err := s.sendEmailVerificationOTP(context.Background(), *user.Email, user.FirstName); err != nil {
				s.logger.Error("send email verification otp on register", zap.Error(err))
			}
		}()
	}

	return pair, nil
}

// ─── Login ────────────────────────────────────────────────────────────────────

type LoginInput struct {
	Phone    string
	PIN      string
	DeviceIP string
	DeviceUA string
}

func (s *AuthService) Login(ctx context.Context, in LoginInput) (*LoginResult, error) {
	q := db.New(s.pool)

	user, err := q.GetUserByPhone(ctx, in.Phone)
	if err != nil {
		return nil, ErrUserNotFound
	}
	if !user.IsActive {
		return nil, ErrAccountInactive
	}
	if user.PinHash == nil {
		return nil, ErrSocialAuthUser
	}
	if !hash.CheckPIN(*user.PinHash, in.PIN) {
		return nil, ErrInvalidPIN
	}

	// If the user has 2FA enabled, issue a short-lived pending ref instead of
	// a full session. The client must complete the TOTP step before getting tokens.
	if sec, err := q.GetUserSecurity(ctx, user.ID); err == nil && sec.TOTPEnabled {
		ref := generateSecureToken()[:16]
		_ = s.rdb.Set(ctx, "2fa:pending:"+ref, user.ID.String(), 5*time.Minute)
		return &LoginResult{Requires2FA: true, TOTPRef: ref}, nil
	}

	pair, err := s.createSession(ctx, q, user.ID, user.PhoneNumber, user.Role, in.DeviceIP, in.DeviceUA)
	if err != nil {
		return nil, err
	}

	if user.Email != nil {
		deviceName := parseDeviceName(in.DeviceUA)
		go func() {
			subj, html := email.LoginNotification(*user.Email, email.LoginNotificationData{
				FirstName:  user.FirstName,
				DeviceName: deviceName,
				DeviceIP:   fallback(in.DeviceIP, "Unknown"),
				DeviceUA:   in.DeviceUA,
				Time:       time.Now().UTC().Format("02 Jan 2006 15:04 UTC"),
			})
			if err := s.emailClient.Send(*user.Email, subj, html); err != nil {
				s.logger.Error("send login notification", zap.Error(err))
			}
		}()
	}

	return &LoginResult{Pair: pair}, nil
}

// CompleteTOTPLogin exchanges a pending 2FA ref + a valid TOTP code for a full JWT pair.
func (s *AuthService) CompleteTOTPLogin(ctx context.Context, ref, code, deviceIP, deviceUA string) (*token.Pair, error) {
	redisKey := "2fa:pending:" + ref
	userIDStr, err := s.rdb.Get(ctx, redisKey).Result()
	if err != nil {
		return nil, ErrInvalidToken
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, ErrUserNotFound
	}

	q := db.New(s.pool)
	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	sec, err := q.GetUserSecurity(ctx, userID)
	if err != nil || !sec.TOTPEnabled || sec.TOTPSecret == nil {
		return nil, ErrInvalidToken
	}
	if !totp.Validate(code, *sec.TOTPSecret) {
		return nil, ErrInvalidToken
	}

	s.rdb.Del(ctx, redisKey)

	return s.createSession(ctx, q, user.ID, user.PhoneNumber, user.Role, deviceIP, deviceUA)
}

// ─── Google Sign-In ───────────────────────────────────────────────────────────

type GoogleSignInInput struct {
	IDToken  string
	DeviceIP string
	DeviceUA string
}

type GoogleSignInResult struct {
	Pair      *token.Pair
	IsNewUser bool
}

// GoogleSignIn verifies a Google ID token, creates or retrieves the user account,
// and returns a JWT pair. New users still need to submit KYC before financial features
// are unlocked (kyc_status starts as "pending").
func (s *AuthService) GoogleSignIn(ctx context.Context, in GoogleSignInInput) (*GoogleSignInResult, error) {
	q := db.New(s.pool)

	info, err := google.VerifyIDToken(ctx, in.IDToken, s.cfg.Google.ClientID)
	if err != nil {
		return nil, ErrInvalidToken
	}

	// Existing user by Google sub.
	if user, err := q.GetUserByGoogleSub(ctx, info.Sub); err == nil {
		pair, err := s.createSession(ctx, q, user.ID, user.PhoneNumber, user.Role, in.DeviceIP, in.DeviceUA)
		if err != nil {
			return nil, err
		}
		go func() {
			if user.Email != nil {
				deviceName := parseDeviceName(in.DeviceUA)
				subj, html := email.LoginNotification(*user.Email, email.LoginNotificationData{
					FirstName:  user.FirstName,
					DeviceName: deviceName,
					DeviceIP:   fallback(in.DeviceIP, "Unknown"),
					DeviceUA:   in.DeviceUA,
					Time:       time.Now().UTC().Format("02 Jan 2006 15:04 UTC"),
				})
				_ = s.emailClient.Send(*user.Email, subj, html)
			}
		}()
		return &GoogleSignInResult{Pair: pair, IsNewUser: false}, nil
	}

	// Existing user by email — link the Google sub.
	if info.Email != "" {
		if user, err := q.GetUserByEmail(ctx, info.Email); err == nil {
			// Link Google sub to existing account going forward.
			sub := info.Sub
			user.GoogleSub = &sub
			pair, err := s.createSession(ctx, q, user.ID, user.PhoneNumber, user.Role, in.DeviceIP, in.DeviceUA)
			if err != nil {
				return nil, err
			}
			return &GoogleSignInResult{Pair: pair, IsNewUser: false}, nil
		}
	}

	// New user — create account with Google as provider.
	googleUser, err := q.CreateGoogleUser(ctx, db.CreateGoogleUserParams{
		Email:        info.Email,
		FirstName:    info.GivenName,
		LastName:     info.FamilyName,
		GoogleSub:    info.Sub,
		Role:         "user",
		AuthProvider: "google",
	})
	if err != nil {
		return nil, fmt.Errorf("create google user: %w", err)
	}

	// Provision default fiat accounts.
	for _, currency := range []string{"XAF", "XOF", "USD", "GBP", "EUR"} {
		if _, err := q.CreateAccount(ctx, db.CreateAccountParams{
			UserID:        googleUser.ID,
			Currency:      currency,
			AccountNumber: generateAccountNumber(),
		}); err != nil {
			s.logger.Error("create account for google user", zap.String("currency", currency), zap.Error(err))
		}
	}

	pair, err := s.createSession(ctx, q, googleUser.ID, googleUser.PhoneNumber, googleUser.Role, in.DeviceIP, in.DeviceUA)
	if err != nil {
		return nil, err
	}

	go s.sendWelcomeEmail(info.Email, googleUser.FirstName)

	return &GoogleSignInResult{Pair: pair, IsNewUser: true}, nil
}

// ─── Logout ───────────────────────────────────────────────────────────────────

func (s *AuthService) Logout(ctx context.Context, userID uuid.UUID, sessionID string) error {
	q := db.New(s.pool)

	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return ErrSessionNotFound
	}

	session, _ := q.GetUserSessionByID(ctx, sid)

	if err := q.RevokeUserSessionByID(ctx, sid, userID); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}

	user, _ := q.GetUserByID(ctx, userID)
	if user.Email != nil && session.ID != uuid.Nil {
		deviceName := "Unknown"
		if session.DeviceName != nil {
			deviceName = *session.DeviceName
		}
		go func() {
			subj, html := email.LogoutNotification(*user.Email, user.FirstName, deviceName,
				time.Now().UTC().Format("02 Jan 2006 15:04 UTC"))
			_ = s.emailClient.Send(*user.Email, subj, html)
		}()
	}

	return nil
}

// ─── Refresh Token ────────────────────────────────────────────────────────────

func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string) (*token.Pair, error) {
	q := db.New(s.pool)

	claims, err := token.Parse(refreshToken, s.cfg.JWT.Secret)
	if err != nil {
		return nil, ErrInvalidToken
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Validate the refresh token against the stored hash — detects token theft.
	rtHash := sha256Hex(refreshToken)
	session, err := q.GetUserSessionByRefreshTokenHash(ctx, rtHash)
	if err != nil {
		return nil, ErrInvalidToken
	}

	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Issue rotated pair with same session ID and role.
	pair, err := token.NewPair(user.ID, user.PhoneNumber, session.ID.String(), user.Role,
		s.cfg.JWT.Secret, s.cfg.JWT.AccessExpiry, s.cfg.JWT.RefreshExpiry)
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	// Rotate: update session to the new refresh token hash.
	_ = q.UpdateSessionRefreshTokenHash(ctx, db.UpdateSessionRefreshTokenHashParams{
		ID:               session.ID,
		RefreshTokenHash: sha256Hex(pair.RefreshToken),
	})

	return pair, nil
}

// ─── Forgot PIN ───────────────────────────────────────────────────────────────

func (s *AuthService) ForgotPIN(ctx context.Context, emailOrPhone string) error {
	q := db.New(s.pool)

	var userEmail, firstName string

	if strings.Contains(emailOrPhone, "@") {
		u, err := q.GetUserByEmail(ctx, emailOrPhone)
		if err != nil || u.Email == nil {
			return nil // silent — don't leak account existence
		}
		userEmail = *u.Email
		firstName = u.FirstName
	} else {
		u, err := q.GetUserByPhone(ctx, emailOrPhone)
		if err != nil || u.Email == nil {
			return nil
		}
		userEmail = *u.Email
		firstName = u.FirstName
	}

	code := fmt.Sprintf("%06d", mrand.Intn(1000000))
	if _, err := q.CreateEmailOTP(ctx, db.CreateEmailOTPParams{
		Email:     userEmail,
		Code:      code,
		Purpose:   "pin_reset",
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}); err != nil {
		return fmt.Errorf("create pin reset otp: %w", err)
	}

	subj, html := email.PINResetOTP(userEmail, firstName, code)
	go func() {
		if err := s.emailClient.Send(userEmail, subj, html); err != nil {
			s.logger.Error("send pin reset otp email", zap.Error(err))
		}
	}()

	return nil
}

// ─── Reset PIN ────────────────────────────────────────────────────────────────

type ResetPINInput struct {
	EmailOrPhone string
	OTPCode      string
	NewPIN       string
	DeviceUA     string
}

func (s *AuthService) ResetPIN(ctx context.Context, in ResetPINInput) error {
	q := db.New(s.pool)

	var userEmail string
	var user db.User

	if strings.Contains(in.EmailOrPhone, "@") {
		u, err := q.GetUserByEmail(ctx, in.EmailOrPhone)
		if err != nil {
			return ErrUserNotFound
		}
		user = u
		if u.Email != nil {
			userEmail = *u.Email
		}
	} else {
		u, err := q.GetUserByPhone(ctx, in.EmailOrPhone)
		if err != nil {
			return ErrUserNotFound
		}
		user = u
		if u.Email != nil {
			userEmail = *u.Email
		}
	}

	otp, err := q.GetValidEmailOTP(ctx, db.GetValidEmailOTPParams{
		Email:   userEmail,
		Code:    in.OTPCode,
		Purpose: "pin_reset",
	})
	if err != nil {
		return ErrInvalidOTP
	}

	_ = q.MarkEmailOTPUsed(ctx, otp.ID)

	pinHash, err := hash.PIN(in.NewPIN)
	if err != nil {
		return fmt.Errorf("hash pin: %w", err)
	}

	if err := q.UpdateUserPinHash(ctx, user.ID, pinHash); err != nil {
		return fmt.Errorf("update pin: %w", err)
	}

	// Force re-login on all devices after PIN change.
	_ = q.RevokeAllUserSessions(ctx, user.ID)

	if userEmail != "" {
		subj, html := email.PINChanged(userEmail, user.FirstName,
			parseDeviceName(in.DeviceUA),
			time.Now().UTC().Format("02 Jan 2006 15:04 UTC"))
		go func() { _ = s.emailClient.Send(userEmail, subj, html) }()
	}

	return nil
}

// ─── Email Verification ───────────────────────────────────────────────────────

func (s *AuthService) SendEmailVerificationOTP(ctx context.Context, userID uuid.UUID) error {
	q := db.New(s.pool)

	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}
	if user.Email == nil {
		return fmt.Errorf("no email on account")
	}

	return s.sendEmailVerificationOTP(ctx, *user.Email, user.FirstName)
}

func (s *AuthService) VerifyEmail(ctx context.Context, userID uuid.UUID, code string) error {
	q := db.New(s.pool)

	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}
	if user.Email == nil {
		return fmt.Errorf("no email on account")
	}

	otp, err := q.GetValidEmailOTP(ctx, db.GetValidEmailOTPParams{
		Email:   *user.Email,
		Code:    code,
		Purpose: "verify_email",
	})
	if err != nil {
		return ErrInvalidOTP
	}

	_ = q.MarkEmailOTPUsed(ctx, otp.ID)

	return q.UpdateUserEmailVerified(ctx, user.ID)
}

// ─── Devices ──────────────────────────────────────────────────────────────────

func (s *AuthService) ListDevices(ctx context.Context, userID uuid.UUID) ([]db.UserSession, error) {
	q := db.New(s.pool)
	return q.ListActiveSessionsByUserID(ctx, userID)
}

func (s *AuthService) DisconnectDevice(ctx context.Context, userID uuid.UUID, sessionID string) error {
	q := db.New(s.pool)
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return ErrSessionNotFound
	}
	return q.RevokeUserSessionByID(ctx, sid, userID)
}

func (s *AuthService) DisconnectAllDevices(ctx context.Context, userID uuid.UUID, currentSessionID string) error {
	q := db.New(s.pool)
	if currentSessionID != "" {
		if sid, err := uuid.Parse(currentSessionID); err == nil {
			return q.RevokeAllOtherSessions(ctx, db.RevokeAllOtherSessionsParams{
				UserID:    userID,
				ExcludeID: sid,
			})
		}
	}
	return q.RevokeAllUserSessions(ctx, userID)
}

// ─── Profile ──────────────────────────────────────────────────────────────────

type UpdateProfileInput struct {
	UserID      uuid.UUID
	FirstName   string
	LastName    string
	Bio         *string
	AvatarURL   *string
	DateOfBirth *string
	Nationality *string
}

func (s *AuthService) GetProfile(ctx context.Context, userID uuid.UUID) (*db.UserFull, error) {
	q := db.New(s.pool)
	user, err := q.GetUserFullByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}
	return &user, nil
}

func (s *AuthService) UpdateProfile(ctx context.Context, in UpdateProfileInput) (*db.User, error) {
	q := db.New(s.pool)
	user, err := q.UpdateUserProfile(ctx, db.UpdateUserProfileParams{
		ID:          in.UserID,
		FirstName:   in.FirstName,
		LastName:    in.LastName,
		Bio:         in.Bio,
		AvatarURL:   in.AvatarURL,
		DateOfBirth: in.DateOfBirth,
		Nationality: in.Nationality,
	})
	if err != nil {
		return nil, fmt.Errorf("update profile: %w", err)
	}
	return &user, nil
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// createSession generates a JWT pair and persists a user_sessions row.
// Because the DB auto-generates the session UUID, we issue the pair twice:
// once to populate the row (any unique RT hash), then again with the real session.ID.
func (s *AuthService) createSession(ctx context.Context, q *db.Queries, userID uuid.UUID, phone, role, deviceIP, deviceUA string) (*token.Pair, error) {
	deviceName := parseDeviceName(deviceUA)

	// First issue: placeholder session ID so we can get the DB row ID.
	placeholder := uuid.New().String()
	tmpPair, err := token.NewPair(userID, phone, placeholder, role,
		s.cfg.JWT.Secret, s.cfg.JWT.AccessExpiry, s.cfg.JWT.RefreshExpiry)
	if err != nil {
		return nil, fmt.Errorf("generate token pair: %w", err)
	}

	session, err := q.CreateUserSession(ctx, db.CreateUserSessionParams{
		UserID:           userID,
		RefreshTokenHash: sha256Hex(tmpPair.RefreshToken),
		DeviceName:       ptrString(deviceName),
		DeviceIP:         ptrString(deviceIP),
		DeviceUA:         ptrString(deviceUA),
		ExpiresAt:        time.Now().Add(s.cfg.JWT.RefreshExpiry),
	})
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	// Reissue with the real session.ID embedded in claims.
	pair, err := token.NewPair(userID, phone, session.ID.String(), role,
		s.cfg.JWT.Secret, s.cfg.JWT.AccessExpiry, s.cfg.JWT.RefreshExpiry)
	if err != nil {
		return nil, fmt.Errorf("reissue token pair: %w", err)
	}

	// Update the stored hash to match the final refresh token.
	_ = q.UpdateSessionRefreshTokenHash(ctx, db.UpdateSessionRefreshTokenHashParams{
		ID:               session.ID,
		RefreshTokenHash: sha256Hex(pair.RefreshToken),
	})

	return pair, nil
}

func (s *AuthService) sendWelcomeEmail(toEmail, firstName string) {
	subj, html := email.Welcome(toEmail, firstName)
	if err := s.emailClient.Send(toEmail, subj, html); err != nil {
		s.logger.Error("send welcome email", zap.String("to", toEmail), zap.Error(err))
	}
}

func (s *AuthService) sendEmailVerificationOTP(ctx context.Context, toEmail, firstName string) error {
	q := db.New(s.pool)
	code := fmt.Sprintf("%06d", mrand.Intn(1000000))
	if _, err := q.CreateEmailOTP(ctx, db.CreateEmailOTPParams{
		Email:     toEmail,
		Code:      code,
		Purpose:   "verify_email",
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}); err != nil {
		return err
	}
	subj, html := email.EmailVerificationOTP(toEmail, firstName, code)
	if err := s.emailClient.Send(toEmail, subj, html); err != nil {
		s.logger.Error("send email otp", zap.String("to", toEmail), zap.Error(err))
	}
	return nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

//nolint:deadcode
func generateSecureToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func parseDeviceName(ua string) string {
	u := strings.ToLower(ua)
	switch {
	case strings.Contains(u, "iphone"):
		return "iPhone"
	case strings.Contains(u, "ipad"):
		return "iPad"
	case strings.Contains(u, "android"):
		return "Android"
	case strings.Contains(u, "windows"):
		return "Windows PC"
	case strings.Contains(u, "macintosh"), strings.Contains(u, "mac os"):
		return "Mac"
	case strings.Contains(u, "linux"):
		return "Linux"
	case u == "":
		return "Unknown Device"
	default:
		return "Browser"
	}
}

func ptrString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func generateAccountNumber() string {
	return fmt.Sprintf("DFX%010d", mrand.Int63n(10000000000))
}
