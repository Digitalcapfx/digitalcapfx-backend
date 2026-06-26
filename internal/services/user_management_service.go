package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

var (
	ErrUserAlreadyDisabled = errors.New("user account is already disabled")
	ErrUserAlreadyActive   = errors.New("user account is already active")
)

// ─── Response types ───────────────────────────────────────────────────────────

type AdminUserListResult struct {
	Users []db.AdminUserView `json:"users"`
	Total int64              `json:"total"`
	Page  int32              `json:"page"`
	Limit int32              `json:"limit"`
}

type AdminUserDetail struct {
	ID           string     `json:"id"`
	PhoneNumber  string     `json:"phone_number"`
	Email        *string    `json:"email"`
	FirstName    string     `json:"first_name"`
	LastName     string     `json:"last_name"`
	FullName     string     `json:"full_name"`
	KycStatus    string     `json:"kyc_status"`
	IsActive     bool       `json:"is_active"`
	Role         string     `json:"role"`
	AuthProvider string     `json:"auth_provider"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	// Linked resources
	FiatAccounts []FiatAccountSummary  `json:"fiat_accounts"`
	WaasWallets  []WaasWalletSummary   `json:"waas_wallets"`
	HasCaasWallet bool                 `json:"has_caas_wallet"`
	// Stats
	TxCount      int64   `json:"tx_count"`
	TxVolume     float64 `json:"tx_volume"`
}

type FiatAccountSummary struct {
	Currency      string  `json:"currency"`
	Balance       string  `json:"balance"`
	AccountNumber string  `json:"account_number"`
	Status        string  `json:"status"`
}

type WaasWalletSummary struct {
	Network   string `json:"network"`
	Address   string `json:"address"`
	IsDefault bool   `json:"is_default"`
}

type AdminUserListFilters struct {
	Search    string
	KycStatus string
	IsActive  *bool
	Page      int32
	Limit     int32
}

type AdminDashboard struct {
	Stats    db.AdminDashboardStats `json:"stats"`
	UpdatedAt time.Time             `json:"updated_at"`
}

// ─── Service ──────────────────────────────────────────────────────────────────

type UserManagementService struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

func NewUserManagementService(pool *pgxpool.Pool, logger *zap.Logger) *UserManagementService {
	return &UserManagementService{pool: pool, logger: logger}
}

// ListUsers returns a paginated, filterable list of all platform users.
func (s *UserManagementService) ListUsers(ctx context.Context, f AdminUserListFilters) (*AdminUserListResult, error) {
	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.Limit

	q := db.New(s.pool)
	params := db.ListUsersForAdminParams{
		Search:    strings.TrimSpace(f.Search),
		KycStatus: f.KycStatus,
		IsActive:  f.IsActive,
		Limit:     f.Limit,
		Offset:    offset,
	}
	users, _ := q.ListUsersForAdmin(ctx, params)
	total, _ := q.CountUsersForAdmin(ctx, params)

	return &AdminUserListResult{Users: users, Total: total, Page: f.Page, Limit: f.Limit}, nil
}

// GetUserDetail returns the full admin view for a single user.
func (s *UserManagementService) GetUserDetail(ctx context.Context, userID uuid.UUID) (*AdminUserDetail, error) {
	q := db.New(s.pool)
	u, err := q.GetUserForAdmin(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Fiat accounts.
	accs, _ := q.GetAccountsByUserID(ctx, userID)
	fiatSummaries := make([]FiatAccountSummary, 0, len(accs))
	for _, a := range accs {
		fiatSummaries = append(fiatSummaries, FiatAccountSummary{
			Currency:      a.Currency,
			Balance:       fmt.Sprintf("%.2f", pgNumericToFloat(a.Balance)),
			AccountNumber: a.AccountNumber,
			Status:        a.Status,
		})
	}

	// WaaS wallets.
	wallets, _ := q.GetWaasWalletsByUserID(ctx, userID)
	waasSummaries := make([]WaasWalletSummary, 0, len(wallets))
	for _, w := range wallets {
		waasSummaries = append(waasSummaries, WaasWalletSummary{
			Network:   w.Network,
			Address:   w.Address,
			IsDefault: w.IsDefault,
		})
	}

	// CaaS wallet check — err == nil means wallet exists.
	_, caasErr := q.GetCaasWalletByUserID(ctx, userID)
	hasCaas := caasErr == nil

	detail := &AdminUserDetail{
		ID:            u.ID.String(),
		PhoneNumber:   u.PhoneNumber,
		Email:         u.Email,
		FirstName:     u.FirstName,
		LastName:      u.LastName,
		FullName:      strings.TrimSpace(u.FirstName + " " + u.LastName),
		KycStatus:     u.KycStatus,
		IsActive:      u.IsActive,
		Role:          u.Role,
		AuthProvider:  u.AuthProvider,
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
		FiatAccounts:  fiatSummaries,
		WaasWallets:   waasSummaries,
		HasCaasWallet: hasCaas,
	}
	return detail, nil
}

// DisableUser soft-disables a user account (sets is_active = false).
// Subsequent logins are blocked in the auth service.
func (s *UserManagementService) DisableUser(ctx context.Context, userID uuid.UUID) error {
	q := db.New(s.pool)
	u, err := q.GetUserForAdmin(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}
	if !u.IsActive {
		return ErrUserAlreadyDisabled
	}
	if err := q.DisableUser(ctx, userID); err != nil {
		return fmt.Errorf("disable user: %w", err)
	}
	s.logger.Info("user disabled", zap.String("user_id", userID.String()))
	return nil
}

// EnableUser re-activates a disabled user account.
func (s *UserManagementService) EnableUser(ctx context.Context, userID uuid.UUID) error {
	q := db.New(s.pool)
	u, err := q.GetUserForAdmin(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}
	if u.IsActive {
		return ErrUserAlreadyActive
	}
	if err := q.EnableUser(ctx, userID); err != nil {
		return fmt.Errorf("enable user: %w", err)
	}
	s.logger.Info("user re-enabled", zap.String("user_id", userID.String()))
	return nil
}

// ResetKYC sets a user's kyc_status back to "pending" so they can resubmit documents.
func (s *UserManagementService) ResetKYC(ctx context.Context, userID uuid.UUID) error {
	q := db.New(s.pool)
	if _, err := q.GetUserForAdmin(ctx, userID); err != nil {
		return ErrUserNotFound
	}
	if err := q.ResetUserKYC(ctx, userID); err != nil {
		return fmt.Errorf("reset kyc: %w", err)
	}
	s.logger.Info("user KYC reset", zap.String("user_id", userID.String()))
	return nil
}

// GetUserTransactions returns paginated fiat transactions for a user (admin view).
func (s *UserManagementService) GetUserTransactions(ctx context.Context, userID uuid.UUID, page, limit int32) ([]db.Transaction, int64, error) {
	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}
	q := db.New(s.pool)
	txns, _ := q.ListTransactionsByUser(ctx, db.ListTransactionsByUserParams{
		UserID: userID,
		Limit:  limit,
		Offset: (page - 1) * limit,
	})
	total, _ := q.CountTransactionsByUser(ctx, userID)
	return txns, total, nil
}

// GetDashboard returns aggregate platform statistics for the admin dashboard.
func (s *UserManagementService) GetDashboard(ctx context.Context) (*AdminDashboard, error) {
	q := db.New(s.pool)
	stats, _ := q.GetAdminDashboardStats(ctx)
	return &AdminDashboard{Stats: stats, UpdatedAt: time.Now()}, nil
}

