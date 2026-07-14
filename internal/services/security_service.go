package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp/totp"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
	"github.com/rachfinance/digitalfx/internal/pkg/hash"
)

var (
	ErrTOTPInvalid       = errors.New("invalid or expired TOTP code")
	ErrTOTPNotEnabled    = errors.New("2FA is not enabled for this account")
	ErrTOTPAlreadyActive = errors.New("2FA is already enabled — disable it first to re-enrol")
	ErrWrongPIN          = errors.New("current PIN is incorrect")
	ErrTOTPSetupExpired  = errors.New("setup session expired — restart 2FA setup")
)

type SecurityService struct {
	pool   *pgxpool.Pool
	rdb    *redis.Client
	logger *zap.Logger
}

func NewSecurityService(pool *pgxpool.Pool, rdb *redis.Client, logger *zap.Logger) *SecurityService {
	return &SecurityService{pool: pool, rdb: rdb, logger: logger}
}

// ─── TOTP 2FA ─────────────────────────────────────────────────────────────────

// TOTPSetupResult is returned to the client; the URI is encoded as a QR code by the app.
type TOTPSetupResult struct {
	Secret string `json:"secret"` // base32 — for manual entry
	URI    string `json:"uri"`    // otpauth:// — scan as QR code
}

// SetupTOTP generates a new TOTP secret and stores it in Redis for 10 minutes.
// The user must call ConfirmTOTP within that window to activate 2FA.
func (s *SecurityService) SetupTOTP(ctx context.Context, userID uuid.UUID) (*TOTPSetupResult, error) {
	q := db.New(s.pool)
	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	sec, secErr := q.GetUserSecurity(ctx, userID)
	if secErr == nil && sec.TOTPEnabled {
		return nil, ErrTOTPAlreadyActive
	}

	accountName := user.PhoneNumber
	if user.Email != nil {
		accountName = *user.Email
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "DigitalFX",
		AccountName: accountName,
	})
	if err != nil {
		return nil, fmt.Errorf("generate totp key: %w", err)
	}

	// Persist the secret in a pending state (totp_enabled=false) on the user
	// record. ConfirmTOTP activates it once a valid code is entered. Stateless —
	// no Redis needed.
	secret := key.Secret()
	if err := q.SetTOTPEnabled(ctx, db.SetTOTPEnabledParams{
		ID:          userID,
		TOTPEnabled: false,
		TOTPSecret:  &secret,
	}); err != nil {
		return nil, fmt.Errorf("store pending totp secret: %w", err)
	}

	return &TOTPSetupResult{Secret: secret, URI: key.URL()}, nil
}

// ConfirmTOTP verifies a TOTP code against the pending secret stored during
// setup and activates 2FA.
func (s *SecurityService) ConfirmTOTP(ctx context.Context, userID uuid.UUID, code string) error {
	q := db.New(s.pool)
	sec, err := q.GetUserSecurity(ctx, userID)
	if err != nil || sec.TOTPSecret == nil || *sec.TOTPSecret == "" {
		return ErrTOTPSetupExpired // no pending secret — run setup first
	}
	if sec.TOTPEnabled {
		return ErrTOTPAlreadyActive
	}
	if !totp.Validate(code, *sec.TOTPSecret) {
		return ErrTOTPInvalid
	}

	if err := q.SetTOTPEnabled(ctx, db.SetTOTPEnabledParams{
		ID:          userID,
		TOTPEnabled: true,
		TOTPSecret:  sec.TOTPSecret,
	}); err != nil {
		return fmt.Errorf("activate 2fa in db: %w", err)
	}

	s.logger.Info("2fa enabled", zap.String("user", userID.String()))
	return nil
}

// DisableTOTP verifies a live TOTP code then deactivates 2FA.
func (s *SecurityService) DisableTOTP(ctx context.Context, userID uuid.UUID, code string) error {
	q := db.New(s.pool)
	sec, err := q.GetUserSecurity(ctx, userID)
	if err != nil || !sec.TOTPEnabled || sec.TOTPSecret == nil {
		return ErrTOTPNotEnabled
	}

	if !totp.Validate(code, *sec.TOTPSecret) {
		return ErrTOTPInvalid
	}

	if err := q.SetTOTPEnabled(ctx, db.SetTOTPEnabledParams{
		ID:          userID,
		TOTPEnabled: false,
		TOTPSecret:  nil,
	}); err != nil {
		return fmt.Errorf("disable 2fa: %w", err)
	}

	s.logger.Info("2fa disabled", zap.String("user", userID.String()))
	return nil
}

// VerifyTOTP validates a code against the user's active 2FA secret.
// Used internally by AuthService.CompleteTOTPLogin.
func (s *SecurityService) VerifyTOTP(ctx context.Context, userID uuid.UUID, code string) error {
	q := db.New(s.pool)
	sec, err := q.GetUserSecurity(ctx, userID)
	if err != nil || !sec.TOTPEnabled || sec.TOTPSecret == nil {
		return ErrTOTPNotEnabled
	}
	if !totp.Validate(code, *sec.TOTPSecret) {
		return ErrTOTPInvalid
	}
	return nil
}

// ─── PIN Change ───────────────────────────────────────────────────────────────

// ChangePIN verifies the current PIN then sets a new one.
// Unlike ResetPIN (which uses an email OTP), this requires the old PIN —
// it is for authenticated users who know their current PIN.
func (s *SecurityService) ChangePIN(ctx context.Context, userID uuid.UUID, currentPIN, newPIN string) error {
	q := db.New(s.pool)
	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}
	if user.PinHash == nil {
		return ErrSocialAuthUser
	}
	if !hash.CheckPIN(*user.PinHash, currentPIN) {
		return ErrWrongPIN
	}

	newHash, err := hash.PIN(newPIN)
	if err != nil {
		return fmt.Errorf("hash new pin: %w", err)
	}

	return q.ChangePIN(ctx, userID, newHash)
}

// ─── Biometrics ───────────────────────────────────────────────────────────────

// SetBiometrics persists the biometrics_enabled flag in user_preferences.
// Actual biometric auth is handled entirely on the device; this flag is a
// preference hint so other devices / the dashboard can reflect the setting.
func (s *SecurityService) SetBiometrics(ctx context.Context, userID uuid.UUID, enabled bool) error {
	q := db.New(s.pool)
	return q.SetBiometricsEnabled(ctx, userID, enabled)
}

// ─── 2FA status ───────────────────────────────────────────────────────────────

type SecurityStatus struct {
	TOTPEnabled       bool `json:"totp_enabled"`
	BiometricsEnabled bool `json:"biometrics_enabled"`
}

func (s *SecurityService) GetStatus(ctx context.Context, userID uuid.UUID) (*SecurityStatus, error) {
	q := db.New(s.pool)

	var totpEnabled bool
	sec, err := q.GetUserSecurity(ctx, userID)
	if err == nil {
		totpEnabled = sec.TOTPEnabled
	}

	var bioEnabled bool
	prefs, err := q.GetUserPreferences(ctx, userID)
	if err == nil {
		bioEnabled = prefs.BiometricsEnabled
	}

	return &SecurityStatus{
		TOTPEnabled:       totpEnabled,
		BiometricsEnabled: bioEnabled,
	}, nil
}
