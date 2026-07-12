package services

import (
	"context"

	"github.com/google/uuid"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

// ─── Business analytics response types ─────────────────────────────────────────

// CurrencyVolume is the transaction activity for one currency over the period.
type CurrencyVolume struct {
	Currency  string  `json:"currency"`
	Count     int64   `json:"count"`
	Volume    float64 `json:"volume"`     // native currency
	VolumeUSD float64 `json:"volume_usd"` // USD-normalised
}

// CurrencyBalance is the current balance of one fiat account.
type CurrencyBalance struct {
	Currency   string  `json:"currency"`
	Balance    float64 `json:"balance"`     // native currency
	BalanceUSD float64 `json:"balance_usd"` // USD-normalised
}

// BusinessTxStats are the headline transaction figures over the period.
type BusinessTxStats struct {
	Count          int64   `json:"count"`
	TotalVolumeUSD float64 `json:"total_volume_usd"`
	AvgTxUSD       float64 `json:"avg_transaction_usd"`
}

// BusinessAnalyticsData is the richer analytics payload reserved for business
// accounts: the full individual insights plus transaction stats, a per-currency
// volume breakdown, and a per-currency balance breakdown.
type BusinessAnalyticsData struct {
	Period           string            `json:"period"`
	Insights         *InsightsData     `json:"insights"`
	TransactionStats BusinessTxStats   `json:"transaction_stats"`
	VolumeByCurrency []CurrencyVolume  `json:"volume_by_currency"`
	AccountBalances  []CurrencyBalance `json:"account_balances"`
}

// GetBusinessAnalytics builds the business-tier analytics payload. It reuses the
// standard insights and layers on business-grade detail: total count/volume,
// average ticket size, and per-currency breakdowns of both activity and
// balances. All USD figures are FX-normalised via defaultFXRates.
func (s *InsightsService) GetBusinessAnalytics(ctx context.Context, userID uuid.UUID, period string) (*BusinessAnalyticsData, error) {
	if period == "" {
		period = "1m"
	}
	since := periodStart(period)
	q := db.New(s.pool)
	rates := defaultFXRates()

	insights, err := s.GetInsights(ctx, userID, period)
	if err != nil {
		return nil, err
	}

	// Per-currency volume breakdown, plus a USD-normalised grand total.
	volRows, _ := q.GetVolumeByCurrencySince(ctx, db.GetVolumeByCurrencySinceParams{UserID: userID, CreatedAt: since})
	volByCcy := make([]CurrencyVolume, 0, len(volRows))
	var totalVolumeUSD float64
	for _, r := range volRows {
		usd := 0.0
		if rate := rates[r.Currency]; rate > 0 {
			usd = r.Volume / rate
		}
		totalVolumeUSD += usd
		volByCcy = append(volByCcy, CurrencyVolume{
			Currency:  r.Currency,
			Count:     r.TxCount,
			Volume:    roundUSD(r.Volume),
			VolumeUSD: roundUSD(usd),
		})
	}

	// Total count (exact, currency-agnostic) and average ticket size in USD.
	stats, _ := q.GetTransactionStatsSince(ctx, db.GetTransactionStatsSinceParams{UserID: userID, CreatedAt: since})
	avgUSD := 0.0
	if stats.TxCount > 0 {
		avgUSD = totalVolumeUSD / float64(stats.TxCount)
	}

	// Current balances per currency.
	accounts, _ := q.GetAccountsByUserID(ctx, userID)
	balances := make([]CurrencyBalance, 0, len(accounts))
	for _, acc := range accounts {
		bal := pgNumericToFloat(acc.Balance)
		usd := 0.0
		if rate := rates[acc.Currency]; rate > 0 {
			usd = bal / rate
		}
		balances = append(balances, CurrencyBalance{
			Currency:   acc.Currency,
			Balance:    roundUSD(bal),
			BalanceUSD: roundUSD(usd),
		})
	}

	return &BusinessAnalyticsData{
		Period:   period,
		Insights: insights,
		TransactionStats: BusinessTxStats{
			Count:          stats.TxCount,
			TotalVolumeUSD: roundUSD(totalVolumeUSD),
			AvgTxUSD:       roundUSD(avgUSD),
		},
		VolumeByCurrency: volByCcy,
		AccountBalances:  balances,
	}, nil
}
