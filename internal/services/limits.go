package services

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// LimitsProvider resolves the effective limits for a specific user. It is the
// abstraction the withdrawal service depends on, so limits can be sourced from
// static defaults (LimitsResolver) or the DB with per-user overrides
// (LimitsService) without changing callers.
type LimitsProvider interface {
	Resolve(ctx context.Context, userID uuid.UUID, accountType string) AccountLimits
}

// AccountLimits describes the caps for one account tier. All monetary values
// are USD-equivalent. A value of 0 means "no limit".
type AccountLimits struct {
	Tier                  string  `json:"tier"` // "individual" | "business"
	DailyWithdrawalUSD    float64 `json:"daily_withdrawal_usd"`
	PerTransactionUSD     float64 `json:"per_transaction_usd"`
	MonthlyVolumeUSD      float64 `json:"monthly_volume_usd"`
	MaxHoldingBalanceUSD  float64 `json:"max_holding_balance_usd"`
	DailyTransactionCount int     `json:"daily_transaction_count"`
}

// LimitsResolver returns the limits for an account tier. Business tiers are
// strictly more generous than individual ones.
type LimitsResolver struct {
	Individual AccountLimits
	Business   AccountLimits
}

// For returns the limits for the given account type (defaults to individual
// for any unknown value).
func (r LimitsResolver) For(accountType string) AccountLimits {
	if strings.EqualFold(strings.TrimSpace(accountType), "business") {
		return r.Business
	}
	return r.Individual
}

// Resolve satisfies LimitsProvider. The static resolver ignores the context and
// user id — it has no per-user overrides — and resolves purely by tier.
func (r LimitsResolver) Resolve(_ context.Context, _ uuid.UUID, accountType string) AccountLimits {
	return r.For(accountType)
}

// DefaultLimitsResolver holds the built-in tier defaults. Each value can be
// overridden at deploy time via the LIMITS_* environment variables (see
// config.LimitsConfig). Individual mirrors the historical $10k/day behaviour;
// business is an order of magnitude higher across the board.
func DefaultLimitsResolver() LimitsResolver {
	return LimitsResolver{
		Individual: AccountLimits{
			Tier:                  "individual",
			DailyWithdrawalUSD:    10_000,
			PerTransactionUSD:     10_000,
			MonthlyVolumeUSD:      100_000,
			MaxHoldingBalanceUSD:  50_000,
			DailyTransactionCount: 50,
		},
		Business: AccountLimits{
			Tier:                  "business",
			DailyWithdrawalUSD:    250_000,
			PerTransactionUSD:     100_000,
			MonthlyVolumeUSD:      5_000_000,
			MaxHoldingBalanceUSD:  5_000_000,
			DailyTransactionCount: 1_000,
		},
	}
}
