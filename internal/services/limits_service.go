package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

// platformLimitsCacheTTL bounds how stale the tier-limits cache can get. Tier
// limits change rarely (admin action), so a short TTL keeps the withdrawal hot
// path fast while still picking up owner edits within seconds.
const platformLimitsCacheTTL = 30 * time.Second

// AccountLimitsOverride is a per-user override. A nil field means "inherit the
// tier limit"; a set field replaces it.
type AccountLimitsOverride struct {
	DailyWithdrawalUSD    *float64 `json:"daily_withdrawal_usd"`
	PerTransactionUSD     *float64 `json:"per_transaction_usd"`
	MonthlyVolumeUSD      *float64 `json:"monthly_volume_usd"`
	MaxHoldingBalanceUSD  *float64 `json:"max_holding_balance_usd"`
	DailyTransactionCount *int     `json:"daily_transaction_count"`
	Note                  string   `json:"note"`
}

// UserLimitsView is the admin's-eye view of one user's limits: what actually
// applies, the tier default it derives from, and any explicit override.
type UserLimitsView struct {
	UserID      string                 `json:"user_id"`
	AccountType string                 `json:"account_type"`
	Effective   AccountLimits          `json:"effective"`
	TierDefault AccountLimits          `json:"tier_default"`
	Override    *AccountLimitsOverride `json:"override"`
}

// LimitsService resolves limits from the DB (platform_limits + per-user
// overrides) and lets admins manage them. It satisfies LimitsProvider. On any
// DB failure it falls back to the hardcoded DefaultLimitsResolver, so limits
// enforcement never silently disappears.
type LimitsService struct {
	pool     *pgxpool.Pool
	fallback LimitsResolver
	logger   *zap.Logger

	mu       sync.RWMutex
	cache    map[string]AccountLimits
	cachedAt time.Time
}

func NewLimitsService(pool *pgxpool.Pool, fallback LimitsResolver, logger *zap.Logger) *LimitsService {
	return &LimitsService{pool: pool, fallback: fallback, logger: logger}
}

// ─── LimitsProvider ────────────────────────────────────────────────────────────

// Resolve returns the effective limits for a user: their tier limits with any
// per-user override applied on top.
func (s *LimitsService) Resolve(ctx context.Context, userID uuid.UUID, accountType string) AccountLimits {
	base := s.tierFor(ctx, accountType)
	ov, err := db.New(s.pool).GetUserLimitOverride(ctx, userID)
	if err != nil {
		return base // no override row (or transient failure) → tier limits
	}
	return applyOverride(base, ov)
}

// ─── Admin: tier limits ────────────────────────────────────────────────────────

// TierLimits returns the current persisted limits for every tier.
func (s *LimitsService) TierLimits(ctx context.Context) ([]AccountLimits, error) {
	rows, err := db.New(s.pool).ListPlatformLimits(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AccountLimits, 0, len(rows))
	for _, r := range rows {
		out = append(out, platformLimitToAccountLimits(r))
	}
	return out, nil
}

// UpdateTier persists new limits for a tier and invalidates the cache.
func (s *LimitsService) UpdateTier(ctx context.Context, tier string, in AccountLimits, updatedBy *uuid.UUID) (AccountLimits, error) {
	tier = strings.ToLower(strings.TrimSpace(tier))
	if tier != "individual" && tier != "business" {
		return AccountLimits{}, fmt.Errorf("invalid tier %q (must be individual or business)", tier)
	}
	if err := validateLimits(in); err != nil {
		return AccountLimits{}, err
	}
	row, err := db.New(s.pool).UpsertPlatformLimit(ctx, db.UpsertPlatformLimitParams{
		Tier:                  tier,
		DailyWithdrawalUsd:    in.DailyWithdrawalUSD,
		PerTransactionUsd:     in.PerTransactionUSD,
		MonthlyVolumeUsd:      in.MonthlyVolumeUSD,
		MaxHoldingBalanceUsd:  in.MaxHoldingBalanceUSD,
		DailyTransactionCount: int32(in.DailyTransactionCount),
		UpdatedBy:             updatedBy,
	})
	if err != nil {
		return AccountLimits{}, err
	}
	s.invalidate()
	return platformLimitToAccountLimits(row), nil
}

// ─── Admin: per-user overrides ─────────────────────────────────────────────────

// UserLimits returns the full admin view of a user's limits.
func (s *LimitsService) UserLimits(ctx context.Context, userID uuid.UUID) (*UserLimitsView, error) {
	q := db.New(s.pool)
	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}
	base := s.tierFor(ctx, user.AccountType)
	effective := base
	var ovView *AccountLimitsOverride
	if ov, err := q.GetUserLimitOverride(ctx, userID); err == nil {
		effective = applyOverride(base, ov)
		ovView = &AccountLimitsOverride{
			DailyWithdrawalUSD:    ov.DailyWithdrawalUsd,
			PerTransactionUSD:     ov.PerTransactionUsd,
			MonthlyVolumeUSD:      ov.MonthlyVolumeUsd,
			MaxHoldingBalanceUSD:  ov.MaxHoldingBalanceUsd,
			DailyTransactionCount: int32PtrToIntPtr(ov.DailyTransactionCount),
			Note:                  ov.Note,
		}
	}
	return &UserLimitsView{
		UserID:      userID.String(),
		AccountType: user.AccountType,
		Effective:   effective,
		TierDefault: base,
		Override:    ovView,
	}, nil
}

// SetUserOverride upserts a per-user override.
func (s *LimitsService) SetUserOverride(ctx context.Context, userID uuid.UUID, ov AccountLimitsOverride, updatedBy *uuid.UUID) error {
	var count *int32
	if ov.DailyTransactionCount != nil {
		c := int32(*ov.DailyTransactionCount)
		count = &c
	}
	_, err := db.New(s.pool).UpsertUserLimitOverride(ctx, db.UpsertUserLimitOverrideParams{
		UserID:                userID,
		DailyWithdrawalUsd:    ov.DailyWithdrawalUSD,
		PerTransactionUsd:     ov.PerTransactionUSD,
		MonthlyVolumeUsd:      ov.MonthlyVolumeUSD,
		MaxHoldingBalanceUsd:  ov.MaxHoldingBalanceUSD,
		DailyTransactionCount: count,
		Note:                  ov.Note,
		UpdatedBy:             updatedBy,
	})
	return err
}

// ClearUserOverride removes a user's override so they fall back to tier limits.
func (s *LimitsService) ClearUserOverride(ctx context.Context, userID uuid.UUID) error {
	return db.New(s.pool).DeleteUserLimitOverride(ctx, userID)
}

// SetAccountType flips a user between individual and business.
func (s *LimitsService) SetAccountType(ctx context.Context, userID uuid.UUID, accountType string) (string, error) {
	accountType = strings.ToLower(strings.TrimSpace(accountType))
	if accountType != "individual" && accountType != "business" {
		return "", fmt.Errorf("invalid account type %q (must be individual or business)", accountType)
	}
	row, err := db.New(s.pool).SetUserAccountType(ctx, db.SetUserAccountTypeParams{ID: userID, AccountType: accountType})
	if err != nil {
		return "", ErrUserNotFound
	}
	return row.AccountType, nil
}

// ─── Internals ─────────────────────────────────────────────────────────────────

// tierMap returns the tier→limits map, from a short-lived cache, falling back to
// the hardcoded defaults if the DB is unavailable.
func (s *LimitsService) tierMap(ctx context.Context) map[string]AccountLimits {
	s.mu.RLock()
	if s.cache != nil && time.Since(s.cachedAt) < platformLimitsCacheTTL {
		m := s.cache
		s.mu.RUnlock()
		return m
	}
	s.mu.RUnlock()

	rows, err := db.New(s.pool).ListPlatformLimits(ctx)
	if err != nil || len(rows) == 0 {
		if err != nil {
			s.logger.Warn("platform limits load failed; using built-in defaults", zap.Error(err))
		}
		return map[string]AccountLimits{
			"individual": s.fallback.Individual,
			"business":   s.fallback.Business,
		}
	}
	m := make(map[string]AccountLimits, len(rows))
	for _, r := range rows {
		m[r.Tier] = platformLimitToAccountLimits(r)
	}
	s.mu.Lock()
	s.cache = m
	s.cachedAt = time.Now()
	s.mu.Unlock()
	return m
}

func (s *LimitsService) tierFor(ctx context.Context, accountType string) AccountLimits {
	m := s.tierMap(ctx)
	key := "individual"
	if strings.EqualFold(strings.TrimSpace(accountType), "business") {
		key = "business"
	}
	if lim, ok := m[key]; ok {
		return lim
	}
	return s.fallback.For(accountType)
}

func (s *LimitsService) invalidate() {
	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()
}

func platformLimitToAccountLimits(r db.PlatformLimit) AccountLimits {
	return AccountLimits{
		Tier:                  r.Tier,
		DailyWithdrawalUSD:    r.DailyWithdrawalUsd,
		PerTransactionUSD:     r.PerTransactionUsd,
		MonthlyVolumeUSD:      r.MonthlyVolumeUsd,
		MaxHoldingBalanceUSD:  r.MaxHoldingBalanceUsd,
		DailyTransactionCount: int(r.DailyTransactionCount),
	}
}

func applyOverride(base AccountLimits, ov db.UserLimitOverride) AccountLimits {
	if ov.DailyWithdrawalUsd != nil {
		base.DailyWithdrawalUSD = *ov.DailyWithdrawalUsd
	}
	if ov.PerTransactionUsd != nil {
		base.PerTransactionUSD = *ov.PerTransactionUsd
	}
	if ov.MonthlyVolumeUsd != nil {
		base.MonthlyVolumeUSD = *ov.MonthlyVolumeUsd
	}
	if ov.MaxHoldingBalanceUsd != nil {
		base.MaxHoldingBalanceUSD = *ov.MaxHoldingBalanceUsd
	}
	if ov.DailyTransactionCount != nil {
		base.DailyTransactionCount = int(*ov.DailyTransactionCount)
	}
	return base
}

func validateLimits(in AccountLimits) error {
	if in.DailyWithdrawalUSD < 0 || in.PerTransactionUSD < 0 || in.MonthlyVolumeUSD < 0 ||
		in.MaxHoldingBalanceUSD < 0 || in.DailyTransactionCount < 0 {
		return fmt.Errorf("limit values cannot be negative")
	}
	return nil
}

func int32PtrToIntPtr(p *int32) *int {
	if p == nil {
		return nil
	}
	v := int(*p)
	return &v
}
