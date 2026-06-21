package services

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/config"
	"github.com/rachfinance/digitalfx/internal/db/sqlc"
	"github.com/rachfinance/digitalfx/internal/pkg/hash"
	"github.com/rachfinance/digitalfx/internal/pkg/token"
)

var (
	ErrUserNotFound    = errors.New("user not found")
	ErrUserExists      = errors.New("user already exists")
	ErrInvalidPIN      = errors.New("invalid pin")
	ErrInvalidOTP      = errors.New("invalid or expired otp")
	ErrAccountInactive = errors.New("account is inactive")
)

type AuthService struct {
	pool   *pgxpool.Pool
	rdb    *redis.Client
	cfg    *config.Config
	logger *zap.Logger
}

func NewAuthService(pool *pgxpool.Pool, rdb *redis.Client, cfg *config.Config, logger *zap.Logger) *AuthService {
	return &AuthService{pool: pool, rdb: rdb, cfg: cfg, logger: logger}
}

type RegisterInput struct {
	Phone     string
	Email     string
	FirstName string
	LastName  string
	PIN       string
}

type LoginInput struct {
	Phone string
	PIN   string
}

func (s *AuthService) SendOTP(ctx context.Context, phone string) error {
	q := db.New(s.pool)

	code := fmt.Sprintf("%06d", rand.Intn(1000000))
	expires := time.Now().Add(10 * time.Minute)

	if _, err := q.CreateOTP(ctx, db.CreateOTPParams{
		PhoneNumber: phone,
		Code:        code,
		ExpiresAt:   expires,
	}); err != nil {
		return fmt.Errorf("create otp: %w", err)
	}

	// TODO: send SMS via provider (Twilio, Termii, etc.)
	s.logger.Info("OTP created", zap.String("phone", phone), zap.String("code", code))
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

func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*token.Pair, error) {
	q := db.New(s.pool)

	if _, err := q.GetUserByPhone(ctx, in.Phone); err == nil {
		return nil, ErrUserExists
	}

	pinHash, err := hash.PIN(in.PIN)
	if err != nil {
		return nil, fmt.Errorf("hash pin: %w", err)
	}

	user, err := q.CreateUser(ctx, db.CreateUserParams{
		PhoneNumber: in.Phone,
		Email:       &in.Email,
		FirstName:   in.FirstName,
		LastName:    in.LastName,
		PinHash:     pinHash,
	})
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	// Provision default accounts for all supported currencies
	currencies := []string{"XAF", "XOF", "USD", "GBP", "EUR"}
	for _, currency := range currencies {
		if _, err := q.CreateAccount(ctx, db.CreateAccountParams{
			UserID:        user.ID,
			Currency:      currency,
			AccountNumber: generateAccountNumber(),
		}); err != nil {
			s.logger.Error("create account", zap.String("currency", currency), zap.Error(err))
		}
	}

	return token.NewPair(user.ID, user.PhoneNumber, s.cfg.JWT.Secret,
		s.cfg.JWT.AccessExpiry, s.cfg.JWT.RefreshExpiry)
}

func (s *AuthService) Login(ctx context.Context, in LoginInput) (*token.Pair, error) {
	q := db.New(s.pool)

	user, err := q.GetUserByPhone(ctx, in.Phone)
	if err != nil {
		return nil, ErrUserNotFound
	}

	if !user.IsActive {
		return nil, ErrAccountInactive
	}

	if !hash.CheckPIN(user.PinHash, in.PIN) {
		return nil, ErrInvalidPIN
	}

	return token.NewPair(user.ID, user.PhoneNumber, s.cfg.JWT.Secret,
		s.cfg.JWT.AccessExpiry, s.cfg.JWT.RefreshExpiry)
}

func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string) (*token.Pair, error) {
	claims, err := token.Parse(refreshToken, s.cfg.JWT.Secret)
	if err != nil {
		return nil, ErrInvalidOTP
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	q := db.New(s.pool)
	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	return token.NewPair(user.ID, user.PhoneNumber, s.cfg.JWT.Secret,
		s.cfg.JWT.AccessExpiry, s.cfg.JWT.RefreshExpiry)
}

func generateAccountNumber() string {
	return fmt.Sprintf("DFX%010d", rand.Int63n(10000000000))
}
