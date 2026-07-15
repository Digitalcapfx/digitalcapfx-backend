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
	"github.com/rachfinance/digitalfx/internal/pkg/sms"
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
	ErrBVNRequired     = errors.New("BVN is required for Nigerian customers")
)

// isNigerianCustomer reports whether a customer should be treated as Nigerian
// (and therefore must provide a BVN to open a Nigerian bank account). Detected
// from the ISO country field or a Nigerian (+234) phone number — the phone is
// already canonicalised to E.164, so local "0…" Nigerian numbers count too.
func isNigerianCustomer(country, normalizedPhone string) bool {
	switch strings.ToUpper(strings.TrimSpace(country)) {
	case "NG", "NGA", "NIGERIA":
		return true
	}
	return strings.HasPrefix(normalizedPhone, "+234")
}

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
	smsClient   *sms.Client
}

func NewAuthService(pool *pgxpool.Pool, rdb *redis.Client, cfg *config.Config, logger *zap.Logger, emailClient *email.Client, smsClient *sms.Client) *AuthService {
	return &AuthService{pool: pool, rdb: rdb, cfg: cfg, logger: logger, emailClient: emailClient, smsClient: smsClient}
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

	// Deliver the OTP via Brevo transactional SMS.
	if s.smsClient != nil {
		go func() {
			if err := s.smsClient.SendOTP(context.Background(), phone, s.cfg.App.Name, code); err != nil {
				s.logger.Error("send OTP SMS failed",
					zap.String("phone", phone),
					zap.Error(err),
				)
			} else {
				s.logger.Info("OTP SMS sent", zap.String("phone", phone))
			}
		}()
	} else {
		// No SMS client configured — log the code in development so devs can test.
		s.logger.Warn("SMS client not configured; OTP will not be delivered",
			zap.String("phone", phone),
			zap.String("code", code),
		)
	}

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
	AccountType string // "individual" (default) | "business"
	Phone       string
	Email       string
	FirstName   string
	LastName    string
	PIN         string
	Country     string // ISO 3166-1 alpha-2
	BVN         string // Nigerian Bank Verification Number (11 digits), optional
	DeviceIP    string
	DeviceUA    string
	// Business accounts only — company-level KYB fields collected at signup.
	CompanyLegalName       string
	CompanyRegistrationNo  string
	Industry               string
	CountryOfIncorporation string
	AnnualRevenue          string
	BusinessWebsite        string
}

func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*token.Pair, error) {
	q := db.New(s.pool)

	// Normalize the phone so signup and login always match regardless of how
	// the client formats it (spaces, dashes, missing "+").
	in.Phone = normalizePhone(in.Phone)

	// Nigerian customers must provide a BVN — the account provider needs it to
	// open their Nigerian bank account.
	if isNigerianCustomer(in.Country, in.Phone) && strings.TrimSpace(in.BVN) == "" {
		return nil, ErrBVNRequired
	}

	if _, err := q.GetUserByPhoneAny(ctx, phoneCandidates(in.Phone)); err == nil {
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
	var countryPtr *string
	if in.Country != "" {
		countryPtr = &in.Country
	}

	var user db.User
	if in.AccountType == "business" {
		user, err = q.CreateBusinessUser(ctx, db.CreateBusinessUserParams{
			PhoneNumber:  in.Phone,
			Email:        emailPtr,
			FirstName:    in.FirstName,
			LastName:     in.LastName,
			PinHash:      &pinHash,
			Country:      countryPtr,
			Role:         "user",
			AuthProvider: "phone",
		})
	} else {
		user, err = q.CreateIndividualUser(ctx, db.CreateIndividualUserParams{
			PhoneNumber:  in.Phone,
			Email:        emailPtr,
			FirstName:    in.FirstName,
			LastName:     in.LastName,
			PinHash:      &pinHash,
			Country:      countryPtr,
			Role:         "user",
			AuthProvider: "phone",
		})
	}
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	if in.AccountType == "business" {
		var websitePtr *string
		if in.BusinessWebsite != "" {
			websitePtr = &in.BusinessWebsite
		}
		if _, err := q.CreateBusinessProfile(ctx, db.CreateBusinessProfileParams{
			UserID:                 user.ID,
			CompanyLegalName:       in.CompanyLegalName,
			CompanyRegistrationNo:  in.CompanyRegistrationNo,
			Industry:               in.Industry,
			CountryOfIncorporation: in.CountryOfIncorporation,
			AnnualRevenue:          in.AnnualRevenue,
			BusinessWebsite:        websitePtr,
		}); err != nil {
			return nil, fmt.Errorf("create business profile: %w", err)
		}
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

	// Persist BVN if supplied at signup (Nigerian bank-account provisioning).
	if bvn := strings.TrimSpace(in.BVN); bvn != "" {
		if _, err := q.SetUserBVN(ctx, db.SetUserBVNParams{ID: user.ID, Bvn: &bvn}); err != nil {
			s.logger.Error("set bvn on register", zap.Error(err))
		}
	}

	pair, _, err := s.createSession(ctx, q, user.ID, user.PhoneNumber, user.Role, user.AccountType, in.DeviceIP, in.DeviceUA)
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

	in.Phone = normalizePhone(in.Phone)
	user, err := q.GetUserByPhoneAny(ctx, phoneCandidates(in.Phone))
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

	// If the user has 2FA enabled, issue a short-lived signed pending token
	// instead of a full session. The client must complete the TOTP step before
	// getting tokens. Stateless — no Redis needed.
	if sec, err := q.GetUserSecurity(ctx, user.ID); err == nil && sec.TOTPEnabled {
		ref, err := token.SignPending2FA(user.ID, s.cfg.JWT.Secret, 5*time.Minute)
		if err != nil {
			return nil, fmt.Errorf("issue 2fa pending token: %w", err)
		}
		return &LoginResult{Requires2FA: true, TOTPRef: ref}, nil
	}

	pair, newDevice, err := s.createSession(ctx, q, user.ID, user.PhoneNumber, user.Role, user.AccountType, in.DeviceIP, in.DeviceUA)
	if err != nil {
		return nil, err
	}

	if user.Email != nil {
		deviceName := parseDeviceName(in.DeviceUA)
		go s.sendLoginEmail(*user.Email, user.FirstName, deviceName, in.DeviceIP, in.DeviceUA, newDevice)
	}

	return &LoginResult{Pair: pair}, nil
}

// CompleteTOTPLogin exchanges a pending 2FA ref + a valid TOTP code for a full JWT pair.
func (s *AuthService) CompleteTOTPLogin(ctx context.Context, ref, code, deviceIP, deviceUA string) (*token.Pair, error) {
	userID, err := token.ParsePending2FA(ref, s.cfg.JWT.Secret)
	if err != nil {
		return nil, ErrInvalidToken
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

	pair, newDevice, err := s.createSession(ctx, q, user.ID, user.PhoneNumber, user.Role, user.AccountType, deviceIP, deviceUA)
	if err != nil {
		return nil, err
	}
	if user.Email != nil {
		deviceName := parseDeviceName(deviceUA)
		go s.sendLoginEmail(*user.Email, user.FirstName, deviceName, deviceIP, deviceUA, newDevice)
	}
	return pair, nil
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
	if user, err := q.GetUserByGoogleSub(ctx, &info.Sub); err == nil {
		pair, newDevice, err := s.createSession(ctx, q, user.ID, user.PhoneNumber, user.Role, user.AccountType, in.DeviceIP, in.DeviceUA)
		if err != nil {
			return nil, err
		}
		if user.Email != nil {
			deviceName := parseDeviceName(in.DeviceUA)
			go s.sendLoginEmail(*user.Email, user.FirstName, deviceName, in.DeviceIP, in.DeviceUA, newDevice)
		}
		return &GoogleSignInResult{Pair: pair, IsNewUser: false}, nil
	}

	// Existing user by email — link the Google sub.
	if info.Email != "" {
		if user, err := q.GetUserByEmail(ctx, &info.Email); err == nil {
			// Link Google sub to existing account going forward.
			sub := info.Sub
			user.GoogleSub = &sub
			pair, newDevice, err := s.createSession(ctx, q, user.ID, user.PhoneNumber, user.Role, user.AccountType, in.DeviceIP, in.DeviceUA)
			if err != nil {
				return nil, err
			}
			if user.Email != nil {
				deviceName := parseDeviceName(in.DeviceUA)
				go s.sendLoginEmail(*user.Email, user.FirstName, deviceName, in.DeviceIP, in.DeviceUA, newDevice)
			}
			return &GoogleSignInResult{Pair: pair, IsNewUser: false}, nil
		}
	}

	// New user — create account with Google as provider.
	googleUser, err := q.CreateGoogleUser(ctx, db.CreateGoogleUserParams{
		Email:        &info.Email,
		FirstName:    info.GivenName,
		LastName:     info.FamilyName,
		GoogleSub:    &info.Sub,
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

	pair, _, err := s.createSession(ctx, q, googleUser.ID, googleUser.PhoneNumber, googleUser.Role, googleUser.AccountType, in.DeviceIP, in.DeviceUA)
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

	if err := q.RevokeUserSessionByID(ctx, db.RevokeUserSessionByIDParams{ID: sid, UserID: userID}); err != nil {
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

// EnsureOwners promotes the configured phone numbers to the "owner" staff role.
// It is the founder-bootstrap: run once at startup, idempotent, and the only way
// an owner account is designated (OWNER_PHONES env, deploy-time only). Users must
// already exist (have registered); non-matching phones are logged and skipped.
func (s *AuthService) EnsureOwners(ctx context.Context, phones []string) {
	if len(phones) == 0 {
		return
	}
	q := db.New(s.pool)
	for _, raw := range phones {
		phone := normalizePhone(raw)
		row, err := q.PromoteUserToOwnerByPhone(ctx, phone)
		if err != nil {
			s.logger.Warn("owner bootstrap: no user for phone (will retry next boot once they register)",
				zap.String("phone", phone))
			continue
		}
		s.logger.Info("owner bootstrap: promoted user to owner",
			zap.String("user_id", row.ID.String()),
			zap.String("phone", row.PhoneNumber),
		)
	}
}

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

	pair.AccountType = user.AccountType
	return pair, nil
}

// ─── Forgot PIN ───────────────────────────────────────────────────────────────

func (s *AuthService) ForgotPIN(ctx context.Context, emailOrPhone string) error {
	q := db.New(s.pool)

	var userEmail, firstName string

	if strings.Contains(emailOrPhone, "@") {
		u, err := q.GetUserByEmail(ctx, &emailOrPhone)
		if err != nil || u.Email == nil {
			return nil // silent — don't leak account existence
		}
		userEmail = *u.Email
		firstName = u.FirstName
	} else {
		u, err := q.GetUserByPhoneAny(ctx, phoneCandidates(emailOrPhone))
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
		u, err := q.GetUserByEmail(ctx, &in.EmailOrPhone)
		if err != nil {
			return ErrUserNotFound
		}
		user = u
		if u.Email != nil {
			userEmail = *u.Email
		}
	} else {
		u, err := q.GetUserByPhoneAny(ctx, phoneCandidates(in.EmailOrPhone))
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

	if err := q.UpdateUserPinHash(ctx, db.UpdateUserPinHashParams{ID: user.ID, PinHash: &pinHash}); err != nil {
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

// EmailVerifyResendCooldown is the minimum wait between verification emails.
const EmailVerifyResendCooldown = 60 * time.Second

// ErrEmailAlreadyVerified indicates the account's email is already verified.
var ErrEmailAlreadyVerified = errors.New("email already verified")

// ResendVerificationResult reports the outcome so the frontend can show a timer.
type ResendVerificationResult struct {
	Sent            bool
	CooldownSeconds int
	RetryAfter      int64 // seconds; >0 when blocked by the cooldown
}

// ResendEmailVerification re-sends the email verification code WITHOUT requiring
// a JWT — for the pre-auth "check your email" onboarding screen. Identify the
// account by email or phone. Rate-limited by EmailVerifyResendCooldown so codes
// aren't wasted, and it never reveals whether an account exists.
func (s *AuthService) ResendEmailVerification(ctx context.Context, emailOrPhone string) (*ResendVerificationResult, error) {
	q := db.New(s.pool)

	var user db.User
	var err error
	if strings.Contains(emailOrPhone, "@") {
		email := strings.ToLower(strings.TrimSpace(emailOrPhone))
		user, err = q.GetUserByEmail(ctx, &email)
	} else {
		user, err = q.GetUserByPhoneAny(ctx, phoneCandidates(emailOrPhone))
	}
	// Unknown account (or no email on file) → pretend it was sent (no leak).
	if err != nil || user.Email == nil {
		return &ResendVerificationResult{Sent: true, CooldownSeconds: int(EmailVerifyResendCooldown.Seconds())}, nil
	}

	if user.IsEmailVerified {
		return nil, ErrEmailAlreadyVerified
	}

	// Cooldown: block if a code was sent too recently.
	if last, lerr := q.GetLatestEmailOTPSentAt(ctx, db.GetLatestEmailOTPSentAtParams{
		Email:   *user.Email,
		Purpose: "verify_email",
	}); lerr == nil {
		if elapsed := time.Since(last); elapsed < EmailVerifyResendCooldown {
			retry := int64((EmailVerifyResendCooldown - elapsed).Seconds()) + 1
			return &ResendVerificationResult{Sent: false, RetryAfter: retry, CooldownSeconds: int(EmailVerifyResendCooldown.Seconds())}, nil
		}
	}

	if err := s.sendEmailVerificationOTP(ctx, *user.Email, user.FirstName); err != nil {
		return nil, err
	}
	return &ResendVerificationResult{Sent: true, CooldownSeconds: int(EmailVerifyResendCooldown.Seconds())}, nil
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
	return q.RevokeUserSessionByID(ctx, db.RevokeUserSessionByIDParams{ID: sid, UserID: userID})
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
	BVN         *string
}

func (s *AuthService) GetProfile(ctx context.Context, userID uuid.UUID) (*db.User, error) {
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
		FirstName:   ptrString(in.FirstName),
		LastName:    ptrString(in.LastName),
		Bio:         in.Bio,
		AvatarURL:   in.AvatarURL,
		DateOfBirth: in.DateOfBirth,
		Nationality: in.Nationality,
		Bvn:         in.BVN,
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
// Returns the token pair and whether the user already had other active sessions
// (used by callers to decide between LoginNotification and NewDeviceAlert).
func (s *AuthService) createSession(ctx context.Context, q *db.Queries, userID uuid.UUID, phone, role, accountType, deviceIP, deviceUA string) (*token.Pair, bool, error) {
	deviceName := parseDeviceName(deviceUA)

	// Check existing sessions before creating — determines which email alert to send.
	existingSessions, _ := q.ListActiveSessionsByUserID(ctx, userID)
	hasOtherSessions := len(existingSessions) > 0

	// First issue: placeholder session ID so we can get the DB row ID.
	placeholder := uuid.New().String()
	tmpPair, err := token.NewPair(userID, phone, placeholder, role,
		s.cfg.JWT.Secret, s.cfg.JWT.AccessExpiry, s.cfg.JWT.RefreshExpiry)
	if err != nil {
		return nil, false, fmt.Errorf("generate token pair: %w", err)
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
		return nil, false, fmt.Errorf("create session: %w", err)
	}

	// Reissue with the real session.ID embedded in claims.
	pair, err := token.NewPair(userID, phone, session.ID.String(), role,
		s.cfg.JWT.Secret, s.cfg.JWT.AccessExpiry, s.cfg.JWT.RefreshExpiry)
	if err != nil {
		return nil, false, fmt.Errorf("reissue token pair: %w", err)
	}

	// Update the stored hash to match the final refresh token.
	_ = q.UpdateSessionRefreshTokenHash(ctx, db.UpdateSessionRefreshTokenHashParams{
		ID:               session.ID,
		RefreshTokenHash: sha256Hex(pair.RefreshToken),
	})

	pair.AccountType = accountType
	return pair, hasOtherSessions, nil
}

// sendLoginEmail fires the appropriate security email after a successful login.
// When the user already had other active sessions (concurrent devices), the more
// specific NewDeviceAlert is sent; otherwise the standard LoginNotification is used.
func (s *AuthService) sendLoginEmail(toEmail, firstName, deviceName, deviceIP, deviceUA string, newDevice bool) {
	loginTime := time.Now().UTC().Format("02 Jan 2006 15:04 UTC")
	var subj, html string
	if newDevice {
		subj, html = email.NewDeviceAlert(toEmail, firstName, deviceName, fallback(deviceIP, "Unknown"), loginTime)
	} else {
		subj, html = email.LoginNotification(toEmail, email.LoginNotificationData{
			FirstName:  firstName,
			DeviceName: deviceName,
			DeviceIP:   fallback(deviceIP, "Unknown"),
			DeviceUA:   deviceUA,
			Time:       loginTime,
		})
	}
	if err := s.emailClient.Send(toEmail, subj, html); err != nil {
		s.logger.Error("send login email", zap.String("to", toEmail), zap.Bool("new_device", newDevice), zap.Error(err))
	}
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

// normalizePhone / canonicalPhone / phoneCandidates live in phone.go.

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func generateAccountNumber() string {
	return fmt.Sprintf("DFX%010d", mrand.Int63n(10000000000))
}

func (s *AuthService) Pool() *pgxpool.Pool {
	return s.pool
}

func (s *AuthService) RegisterMerchantStaff(ctx context.Context, token string, phone string, firstName string, lastName string, pin string, deviceIP, deviceUA string) (*token.Pair, error) {
	q := db.New(s.pool)

	staff, err := q.GetMerchantStaffByInviteToken(ctx, &token)
	if err != nil {
		return nil, ErrInvalidInviteToken
	}

	pinHash, err := hash.PIN(pin)
	if err != nil {
		return nil, fmt.Errorf("hash pin: %w", err)
	}

	emailPtr := &staff.Email
	user, err := q.CreateIndividualUser(ctx, db.CreateIndividualUserParams{
		PhoneNumber:  phone,
		Email:        emailPtr,
		FirstName:    firstName,
		LastName:     lastName,
		PinHash:      &pinHash,
		Role:         "user",
		AuthProvider: "phone",
	})
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	for _, currency := range []string{"XAF", "XOF", "USD", "GBP", "EUR"} {
		if _, err := q.CreateAccount(ctx, db.CreateAccountParams{
			UserID:        user.ID,
			Currency:      currency,
			AccountNumber: generateAccountNumber(),
		}); err != nil {
			s.logger.Error("create account for merchant staff", zap.String("currency", currency), zap.Error(err))
		}
	}

	err = q.AcceptMerchantStaffInvite(ctx, db.AcceptMerchantStaffInviteParams{
		StaffUserID: &user.ID,
		InviteToken: &token,
	})
	if err != nil {
		return nil, fmt.Errorf("link staff user: %w", err)
	}

	pair, _, err := s.createSession(ctx, q, user.ID, user.PhoneNumber, user.Role, user.AccountType, deviceIP, deviceUA)
	if err != nil {
		return nil, err
	}

	return pair, nil
}
