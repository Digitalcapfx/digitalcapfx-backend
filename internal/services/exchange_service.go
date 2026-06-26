package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/nilos"
	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

var (
	ErrSameCurrency        = errors.New("exchange: source and target currency must differ")
	ErrUnsupportedPair     = errors.New("exchange: currency pair not supported")
	ErrQuoteExpired        = errors.New("exchange: quote has expired")
)

// ─── Response types ───────────────────────────────────────────────────────────

// ExchangeRate is the live rate preview shown before the user enters an amount.
type ExchangeRate struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Rate      float64   `json:"rate"`       // units of To per 1 unit of From
	RateLabel string    `json:"rate_label"` // "1 USD ≈ 0.925926 EUR"
	QuoteID   string    `json:"quote_id"`   // short-lived — can be passed to execute
	ExpiresAt time.Time `json:"expires_at"`
	Source    string    `json:"source"` // "nilos" | "fallback"
}

// ExchangeQuote is returned when the user has entered an amount.
type ExchangeQuote struct {
	QuoteID      string    `json:"quote_id"`
	From         string    `json:"from"`
	To           string    `json:"to"`
	Rate         float64   `json:"rate"`
	FromAmount   float64   `json:"from_amount"`
	ToAmount     float64   `json:"to_amount"`
	Fee          float64   `json:"fee"`
	FeeFormatted string    `json:"fee_formatted"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// ExchangeResult is returned after a successful swap execution.
type ExchangeResult struct {
	ID          string    `json:"id"`
	Reference   string    `json:"reference"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	Rate        float64   `json:"rate"`
	FromAmount  float64   `json:"from_amount"`
	ToAmount    float64   `json:"to_amount"`
	Fee         float64   `json:"fee"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

// ExchangeHistoryItem is one row in the exchange history list.
type ExchangeHistoryItem struct {
	ID            string    `json:"id"`
	Reference     string    `json:"reference"`
	From          string    `json:"from"`
	To            string    `json:"to"`
	FromAmount    float64   `json:"from_amount"`
	ToAmount      float64   `json:"to_amount"`
	FromFormatted string    `json:"from_formatted"` // "€1,000.00"
	ToFormatted   string    `json:"to_formatted"`   // "$1,080.00"
	Rate          float64   `json:"rate"`
	RateLabel     string    `json:"rate_label"` // "1 EUR = 1.08 USD"
	Status        string    `json:"status"`
	Period        string    `json:"period"` // "this_week" | "last_week" | "earlier"
	CreatedAt     time.Time `json:"created_at"`
}

// ExchangeHistoryGroup groups items by time period.
type ExchangeHistoryGroup struct {
	Period string                `json:"period"`
	Label  string                `json:"label"` // "THIS WEEK"
	Count  int                   `json:"count"`
	Items  []ExchangeHistoryItem `json:"items"`
}

// ExchangeStats is the summary banner at the top of Exchange History.
type ExchangeStats struct {
	TotalExchanges int64   `json:"total_exchanges"`
	Volume         float64 `json:"volume"`
	VolumeFormatted string `json:"volume_formatted"`
	FeesPaid       float64 `json:"fees_paid"`
	FeesPaidFormatted string `json:"fees_paid_formatted"`
}

// ExchangeHistoryResult is the full history response.
type ExchangeHistoryResult struct {
	Stats  ExchangeStats          `json:"stats"`
	Groups []ExchangeHistoryGroup `json:"groups"`
	Total  int64                  `json:"total"`
	Page   int32                  `json:"page"`
	Limit  int32                  `json:"limit"`
}

// ─── Service ──────────────────────────────────────────────────────────────────

type ExchangeService struct {
	pool        *pgxpool.Pool
	nilosClient *nilos.Client
	logger      *zap.Logger
}

func NewExchangeService(pool *pgxpool.Pool, nilosClient *nilos.Client, logger *zap.Logger) *ExchangeService {
	return &ExchangeService{pool: pool, nilosClient: nilosClient, logger: logger}
}

// ─── GetRate — live rate for 1 unit of From ───────────────────────────────────

func (s *ExchangeService) GetRate(ctx context.Context, from, to string) (*ExchangeRate, error) {
	from, to = strings.ToUpper(from), strings.ToUpper(to)
	if from == to {
		return nil, ErrSameCurrency
	}
	if !isSupportedCurrency(from) || !isSupportedCurrency(to) {
		return nil, ErrUnsupportedPair
	}

	quote, err := s.nilosClient.CreateQuote(ctx, nilos.CreateQuoteRequest{
		SourceCurrency: from,
		SourceRail:     currencyRail(from),
		TargetCurrency: to,
		TargetRail:     currencyRail(to),
		Amount:         1,
		Side:           nilos.SideSell,
	})
	if err != nil {
		s.logger.Warn("exchange: nilos quote failed, using fallback rate",
			zap.String("from", from), zap.String("to", to), zap.Error(err))
		rate := fallbackRate(from, to)
		return &ExchangeRate{
			From:      from,
			To:        to,
			Rate:      rate,
			RateLabel: fmt.Sprintf("1 %s ≈ %g %s", from, rate, to),
			Source:    "fallback",
			ExpiresAt: time.Now().Add(30 * time.Second),
		}, nil
	}

	rate := quote.Rate
	if rate == 0 && quote.SourceAmount > 0 {
		rate = quote.TargetAmount / quote.SourceAmount
	}

	return &ExchangeRate{
		From:      from,
		To:        to,
		Rate:      rate,
		RateLabel: fmt.Sprintf("1 %s ≈ %g %s", from, roundTo(rate, 6), to),
		QuoteID:   quote.ID,
		ExpiresAt: quote.ExpiresAt,
		Source:    "nilos",
	}, nil
}

// ─── GetQuote — quote for a specific amount ───────────────────────────────────

func (s *ExchangeService) GetQuote(ctx context.Context, from, to string, amount float64, side string) (*ExchangeQuote, error) {
	from, to = strings.ToUpper(from), strings.ToUpper(to)
	if from == to {
		return nil, ErrSameCurrency
	}
	if !isSupportedCurrency(from) || !isSupportedCurrency(to) {
		return nil, ErrUnsupportedPair
	}
	if side == "" {
		side = nilos.SideSell
	}

	quote, err := s.nilosClient.CreateQuote(ctx, nilos.CreateQuoteRequest{
		SourceCurrency: from,
		SourceRail:     currencyRail(from),
		TargetCurrency: to,
		TargetRail:     currencyRail(to),
		Amount:         amount,
		Side:           side,
	})
	if err != nil {
		// Fallback: compute locally so the UI doesn't break.
		rate := fallbackRate(from, to)
		toAmount := roundTo(amount*rate, 2)
		return &ExchangeQuote{
			QuoteID:      "",
			From:         from,
			To:           to,
			Rate:         rate,
			FromAmount:   amount,
			ToAmount:     toAmount,
			Fee:          0,
			FeeFormatted: "0.00",
			ExpiresAt:    time.Now().Add(30 * time.Second),
		}, nil
	}

	rate := quote.Rate
	if rate == 0 && quote.SourceAmount > 0 {
		rate = quote.TargetAmount / quote.SourceAmount
	}

	return &ExchangeQuote{
		QuoteID:      quote.ID,
		From:         from,
		To:           to,
		Rate:         rate,
		FromAmount:   quote.SourceAmount,
		ToAmount:     quote.TargetAmount,
		Fee:          0,
		FeeFormatted: "0.00",
		ExpiresAt:    quote.ExpiresAt,
	}, nil
}

// ─── Execute — swap and record ────────────────────────────────────────────────

type ExecuteExchangeInput struct {
	UserID   uuid.UUID
	From     string
	To       string
	Amount   float64
	Side     string // "" | "SELL" | "BUY"
	QuoteID  string // optional — if provided skips re-quoting
}

func (s *ExchangeService) Execute(ctx context.Context, in ExecuteExchangeInput) (*ExchangeResult, error) {
	in.From, in.To = strings.ToUpper(in.From), strings.ToUpper(in.To)
	if in.From == in.To {
		return nil, ErrSameCurrency
	}
	if !isSupportedCurrency(in.From) || !isSupportedCurrency(in.To) {
		return nil, ErrUnsupportedPair
	}
	if in.Side == "" {
		in.Side = nilos.SideSell
	}

	q := db.New(s.pool)

	// Look up source Nilos account.
	srcAcc, err := q.GetAccountWithNilosByUserAndCurrency(ctx, in.UserID, in.From)
	if err != nil {
		return nil, ErrAccountNotFound
	}

	// Ensure the target account exists in our DB (the user must hold that currency).
	dstAcc, err := q.GetAccountWithNilosByUserAndCurrency(ctx, in.UserID, in.To)
	if err != nil {
		return nil, fmt.Errorf("exchange: no %s account — open one first", in.To)
	}

	// Check balance.
	srcBal := parseFloatSafe(srcAcc.Balance)
	if in.Side == nilos.SideSell && srcBal < in.Amount {
		return nil, ErrInsufficientFunds
	}

	// NilosAccountID is nullable — guard before use.
	if srcAcc.NilosAccountID == nil || *srcAcc.NilosAccountID == "" {
		return nil, fmt.Errorf("exchange: source account not linked to Nilos")
	}

	// Execute swap via Nilos.
	payout, err := s.nilosClient.CreatePayoutSwap(ctx, nilos.CreatePayoutSwapRequest{
		AccountID:      *srcAcc.NilosAccountID,
		Amount:         in.Amount,
		SourceCurrency: in.From,
		TargetCurrency: in.To,
		Side:           in.Side,
	})
	if err != nil {
		return nil, fmt.Errorf("exchange: nilos swap failed: %w", err)
	}

	rate := 0.0
	if payout.Amount > 0 {
		rate = roundTo(payout.TargetAmount/payout.Amount, 6)
	}

	reference := fmt.Sprintf("EXC-%s", strings.ToUpper(uuid.New().String()[:8]))
	desc := fmt.Sprintf("Exchange %s → %s", in.From, in.To)

	// Build metadata stored on both transaction legs.
	meta, _ := json.Marshal(map[string]any{
		"from_currency":   in.From,
		"to_currency":     in.To,
		"from_amount":     payout.Amount,
		"to_amount":       payout.TargetAmount,
		"rate":            rate,
		"nilos_payout_id": payout.ID,
		"fee":             0,
	})

	txID := uuid.New()

	// Debit leg on source account.
	_, _ = q.CreateFiatTransaction(ctx, db.CreateFiatTransactionParams{
		ID:          txID,
		AccountID:   srcAcc.ID,
		Reference:   reference,
		Type:        "exchange",
		Amount:      fmt.Sprintf("%.8f", payout.Amount),
		Currency:    in.From,
		Fee:         "0.00",
		Description: &desc,
		Status:      payout.Status,
		Metadata:    meta,
	})

	// Credit leg on target account.
	_, _ = q.CreateFiatTransaction(ctx, db.CreateFiatTransactionParams{
		ID:          uuid.New(),
		AccountID:   dstAcc.ID,
		Reference:   reference + "-CR",
		Type:        "exchange",
		Amount:      fmt.Sprintf("%.8f", payout.TargetAmount),
		Currency:    in.To,
		Fee:         "0.00",
		Description: &desc,
		Status:      payout.Status,
		Metadata:    meta,
	})

	return &ExchangeResult{
		ID:         txID.String(),
		Reference:  reference,
		From:       in.From,
		To:         in.To,
		Rate:       rate,
		FromAmount: payout.Amount,
		ToAmount:   payout.TargetAmount,
		Fee:        0,
		Status:     payout.Status,
		CreatedAt:  time.Now(),
	}, nil
}

// ─── Exchange History ─────────────────────────────────────────────────────────

func (s *ExchangeService) GetHistory(ctx context.Context, userID uuid.UUID, page, limit int32) (*ExchangeHistoryResult, error) {
	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	q := db.New(s.pool)

	txns, _ := q.ListExchangesByUser(ctx, db.ListExchangesByUserParams{
		UserID: userID,
		Limit:  limit,
		Offset: offset,
	})
	total, _ := q.CountExchangesByUser(ctx, userID)
	stats, _ := q.GetExchangeStats(ctx, userID)

	items := make([]ExchangeHistoryItem, 0, len(txns))
	for _, t := range txns {
		items = append(items, mapExchangeTx(t))
	}

	groups := groupExchangeByPeriod(items)

	return &ExchangeHistoryResult{
		Stats: ExchangeStats{
			TotalExchanges:    stats.TotalExchanges,
			Volume:            stats.TotalVolume,
			VolumeFormatted:   formatNumber(stats.TotalVolume, 2),
			FeesPaid:          stats.TotalFees,
			FeesPaidFormatted: formatNumber(stats.TotalFees, 2),
		},
		Groups: groups,
		Total:  total,
		Page:   page,
		Limit:  limit,
	}, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

var supportedFiatCurrencies = map[string]bool{
	"USD": true, "EUR": true, "GBP": true, "XAF": true, "XOF": true,
}

func isSupportedCurrency(c string) bool {
	return supportedFiatCurrencies[c]
}

func currencyRail(c string) string {
	switch c {
	case "USD":
		return nilos.RailSWIFT
	case "EUR":
		return nilos.RailSEPA
	case "GBP":
		return nilos.RailFPS
	case "XAF":
		return nilos.RailCEMACBank
	case "XOF":
		return nilos.RailUEMOA
	default:
		return nilos.RailSWIFT
	}
}

// fallbackRate returns a hardcoded USD-anchored rate when Nilos is unavailable.
// Rates are expressed as units-of-To per 1-unit-of-From.
func fallbackRate(from, to string) float64 {
	rates := defaultFXRates()
	// Convert both to USD, then to target.
	fromUSD := 1.0 / rates[from]
	toUSD := rates[to]
	if toUSD == 0 {
		return 0
	}
	return roundTo(fromUSD/toUSD, 6)
}

func roundTo(v float64, places int) float64 {
	factor := 1.0
	for i := 0; i < places; i++ {
		factor *= 10
	}
	return float64(int64(v*factor+0.5)) / factor
}

func mapExchangeTx(t db.Transaction) ExchangeHistoryItem {
	var meta struct {
		FromCurrency string  `json:"from_currency"`
		ToCurrency   string  `json:"to_currency"`
		FromAmount   float64 `json:"from_amount"`
		ToAmount     float64 `json:"to_amount"`
		Rate         float64 `json:"rate"`
	}
	_ = json.Unmarshal(t.Metadata, &meta)

	period, _ := txPeriod(t.CreatedAt)

	fromFmt := fiatFormatted(meta.FromAmount, meta.FromCurrency)
	toFmt := fiatFormatted(meta.ToAmount, meta.ToCurrency)
	rateLabel := fmt.Sprintf("1 %s = %g %s", meta.FromCurrency, meta.Rate, meta.ToCurrency)

	return ExchangeHistoryItem{
		ID:            t.ID.String(),
		Reference:     t.Reference,
		From:          meta.FromCurrency,
		To:            meta.ToCurrency,
		FromAmount:    meta.FromAmount,
		ToAmount:      meta.ToAmount,
		FromFormatted: fromFmt,
		ToFormatted:   toFmt,
		Rate:          meta.Rate,
		RateLabel:     rateLabel,
		Status:        t.Status,
		Period:        period,
		CreatedAt:     t.CreatedAt,
	}
}

func groupExchangeByPeriod(items []ExchangeHistoryItem) []ExchangeHistoryGroup {
	order := []string{"this_week", "last_week", "earlier"}
	labels := map[string]string{"this_week": "THIS WEEK", "last_week": "LAST WEEK", "earlier": "EARLIER"}
	buckets := map[string][]ExchangeHistoryItem{}
	for _, item := range items {
		buckets[item.Period] = append(buckets[item.Period], item)
	}
	var groups []ExchangeHistoryGroup
	for _, period := range order {
		if len(buckets[period]) > 0 {
			groups = append(groups, ExchangeHistoryGroup{
				Period: period,
				Label:  labels[period],
				Count:  len(buckets[period]),
				Items:  buckets[period],
			})
		}
	}
	return groups
}
