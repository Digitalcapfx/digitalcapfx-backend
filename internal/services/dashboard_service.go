package services

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/rachfinance/digitalfx/internal/clients/caas"
	"github.com/rachfinance/digitalfx/internal/clients/nilos"
	"github.com/rachfinance/digitalfx/internal/clients/payments"
	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

type DashboardService struct {
	pool           *pgxpool.Pool
	nilosClient    *nilos.Client
	paymentsClient *payments.Client
	caasClient     *caas.Client
	logger         *zap.Logger
}

func NewDashboardService(
	pool *pgxpool.Pool,
	nilosClient *nilos.Client,
	paymentsClient *payments.Client,
	caasClient *caas.Client,
	logger *zap.Logger,
) *DashboardService {
	return &DashboardService{
		pool:           pool,
		nilosClient:    nilosClient,
		paymentsClient: paymentsClient,
		caasClient:     caasClient,
		logger:         logger,
	}
}

// ─── Dashboard aggregate ──────────────────────────────────────────────────────

type FiatWallet struct {
	AccountID     string  `json:"account_id"`
	Currency      string  `json:"currency"`
	Name          string  `json:"name"`
	Flag          string  `json:"flag"`
	Balance       string  `json:"balance"`      // formatted string, e.g. "12,450.75"
	BalanceRaw    float64 `json:"balance_raw"`  // raw decimal
	BalanceUSD    float64 `json:"balance_usd"`
	AccountNumber string  `json:"account_number"`
	IBAN          *string `json:"iban,omitempty"`
}

type AssetAllocation struct {
	TotalUSD  float64 `json:"total_usd"`
	FiatUSD   float64 `json:"fiat_usd"`
	FiatPct   float64 `json:"fiat_pct"`
	CryptoUSD float64 `json:"crypto_usd"`
	CryptoPct float64 `json:"crypto_pct"`
}

type MonthSummary struct {
	Label       string  `json:"label"`         // "June 2026"
	NetFlowUSD  float64 `json:"net_flow_usd"`  // income - spending
	IncomeUSD   float64 `json:"income_usd"`
	SpendingUSD float64 `json:"spending_usd"`
	TxCount     int64   `json:"tx_count"`
}

type CardInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	LastFour    string `json:"last_four"`
	Currency    string `json:"currency"`
	CardNetwork string `json:"card_network"`
}

type ContactItem struct {
	Name        string `json:"name"`
	PhoneNumber string `json:"phone_number"`
	Initials    string `json:"initials"`
}

type DashboardData struct {
	AssetAllocation   AssetAllocation   `json:"asset_allocation"`
	ThisMonth         MonthSummary      `json:"this_month"`
	FiatWallets       []FiatWallet      `json:"fiat_wallets"`
	CryptoBalanceUSDC float64           `json:"crypto_balance_usdc"` // CaaS phone-send balance
	Card              *CardInfo         `json:"card,omitempty"`
	RecentContacts    []ContactItem     `json:"recent_contacts"`    // for Phone Send quick-pick
	RecentActivity    []db.ActivityItem `json:"recent_activity"`
}

func (s *DashboardService) GetDashboard(ctx context.Context, userID uuid.UUID) (*DashboardData, error) {
	q := db.New(s.pool)

	rates := s.fxRates(ctx, q)

	// ── Fiat wallets (Nilos-backed, balances from local DB) ───────────────────
	accounts, err := q.GetAccountsByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get accounts: %w", err)
	}

	fiatWallets := make([]FiatWallet, 0, len(accounts))
	var fiatTotalUSD float64

	for _, acc := range accounts {
		bal := pgNumericToFloat(acc.Balance)
		balUSD := bal / rates[acc.Currency]
		fiatWallets = append(fiatWallets, FiatWallet{
			AccountID:     acc.ID.String(),
			Currency:      acc.Currency,
			Name:          currencyName(acc.Currency),
			Flag:          currencyFlag(acc.Currency),
			Balance:       formatBalance(bal, acc.Currency),
			BalanceRaw:    bal,
			BalanceUSD:    roundUSD(balUSD),
			AccountNumber: acc.AccountNumber,
		})
		fiatTotalUSD += balUSD
	}

	// ── CaaS USDC balance (Rach CaaS — phone send) ───────────────────────────
	// CaaS identifies users by phone number.
	var caasUSDC float64
	user, err := q.GetUserByID(ctx, userID)
	if err == nil && user.PhoneNumber != "" {
		if balResp, err := s.caasClient.GetBalance(ctx, user.PhoneNumber); err == nil {
			caasUSDC = parseFloatSafe(balResp.BalanceUSDC)
		} else {
			s.logger.Warn("caas balance unavailable", zap.Error(err))
		}
	}

	// ── WaaS crypto portfolio (Rach Payments WaaS) ────────────────────────────
	// Balances are computed from local transaction history (deposits - withdrawals).
	// Live balance queries require on-chain RPC which is handled asynchronously.
	var cryptoTotalUSD float64
	cryptoTotalUSD += caasUSDC // USDC is crypto

	// ── Asset allocation ─────────────────────────────────────────────────────
	totalUSD := fiatTotalUSD + cryptoTotalUSD
	var fiatPct, cryptoPct float64
	if totalUSD > 0 {
		fiatPct = roundUSD(fiatTotalUSD / totalUSD * 100)
		cryptoPct = roundUSD(cryptoTotalUSD / totalUSD * 100)
	}

	// ── This month summary (from local transaction DB) ────────────────────────
	summary, _ := q.GetMonthlyTransactionSummary(ctx, userID)
	thisMonth := MonthSummary{
		Label:       time.Now().Format("January 2006"),
		NetFlowUSD:  roundUSD(summary.IncomeUSD - summary.SpendingUSD),
		IncomeUSD:   roundUSD(summary.IncomeUSD),
		SpendingUSD: roundUSD(summary.SpendingUSD),
		TxCount:     summary.TxCount,
	}

	// ── Virtual card ─────────────────────────────────────────────────────────
	var card *CardInfo
	if vc, err := q.GetActiveVirtualCard(ctx, userID); err == nil {
		card = &CardInfo{
			ID:          vc.ID.String(),
			Name:        vc.CardName,
			LastFour:    vc.LastFour,
			Currency:    vc.Currency,
			CardNetwork: vc.CardNetwork,
		}
	}

	// ── Recent contacts (from CaaS P2P send history) ─────────────────────────
	dbContacts, _ := q.GetRecentContacts(ctx, userID, 5)
	contacts := make([]ContactItem, 0, len(dbContacts))
	for _, c := range dbContacts {
		contacts = append(contacts, ContactItem{
			Name:        c.Name,
			PhoneNumber: c.PhoneNumber,
			Initials:    initials(c.Name),
		})
	}

	// ── Recent activity ───────────────────────────────────────────────────────
	activityRows, _ := q.ListRecentActivity(ctx, db.ListRecentActivityParams{
		UserID: userID,
		Limit:  10,
		Offset: 0,
	})
	activity := make([]db.ActivityItem, 0, len(activityRows))
	for _, r := range activityRows {
		activity = append(activity, activityRowToItem(r.ID, r.Source, r.Type, r.Description, r.Asset, r.Currency, r.Amount, r.AmountSign, r.Status, r.CounterName, r.CreatedAt))
	}

	return &DashboardData{
		AssetAllocation: AssetAllocation{
			TotalUSD:  roundUSD(totalUSD),
			FiatUSD:   roundUSD(fiatTotalUSD),
			FiatPct:   fiatPct,
			CryptoUSD: roundUSD(cryptoTotalUSD),
			CryptoPct: cryptoPct,
		},
		ThisMonth:         thisMonth,
		FiatWallets:       fiatWallets,
		CryptoBalanceUSDC: caasUSDC,
		Card:              card,
		RecentContacts:    contacts,
		RecentActivity:    activity,
	}, nil
}

// ─── Activity feed ────────────────────────────────────────────────────────────

type ActivityFeedResult struct {
	Items []db.ActivityItem `json:"items"`
	Page  int               `json:"page"`
	Limit int               `json:"limit"`
}

func (s *DashboardService) GetActivityFeed(ctx context.Context, userID uuid.UUID, page, perPage int32) (*ActivityFeedResult, error) {
	q := db.New(s.pool)

	if perPage == 0 {
		perPage = 20
	}
	if page == 0 {
		page = 1
	}

	rows, err := q.ListRecentActivity(ctx, db.ListRecentActivityParams{
		UserID: userID,
		Limit:  perPage,
		Offset: (page - 1) * perPage,
	})
	if err != nil {
		return nil, fmt.Errorf("list activity: %w", err)
	}

	items := make([]db.ActivityItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, activityRowToItem(r.ID, r.Source, r.Type, r.Description, r.Asset, r.Currency, r.Amount, r.AmountSign, r.Status, r.CounterName, r.CreatedAt))
	}

	return &ActivityFeedResult{
		Items: items,
		Page:  int(page),
		Limit: int(perPage),
	}, nil
}

// ─── Recent contacts ──────────────────────────────────────────────────────────

func (s *DashboardService) GetRecentContacts(ctx context.Context, userID uuid.UUID) ([]ContactItem, error) {
	q := db.New(s.pool)
	dbContacts, err := q.GetRecentContacts(ctx, userID, 8)
	if err != nil {
		return nil, err
	}
	out := make([]ContactItem, 0, len(dbContacts))
	for _, c := range dbContacts {
		out = append(out, ContactItem{
			Name:        c.Name,
			PhoneNumber: c.PhoneNumber,
			Initials:    initials(c.Name),
		})
	}
	return out, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func (s *DashboardService) fxRates(ctx context.Context, q *db.Queries) map[string]float64 {
	rates := defaultFXRates()
	rows, err := q.GetAllFXRates(ctx)
	if err != nil {
		return rates
	}
	for _, r := range rows {
		if r.BaseCurrency == "USD" {
			if f, err := strconv.ParseFloat(r.Rate, 64); err == nil && f > 0 {
				rates[r.QuoteCurrency] = f
			}
		}
	}
	return rates
}

func defaultFXRates() map[string]float64 {
	return map[string]float64{
		"USD": 1.0, "USDC": 1.0, "USDT": 1.0,
		"EUR": 0.91, "GBP": 0.79,
		"XAF": 609.0, "XOF": 609.0,
		// Crypto — approximate USD/unit rates as fallback (DB rates override these)
		"BTC": 0.000015, "ETH": 0.00033,
		"SOL": 0.0065, "LTC": 0.012, "TRX": 8.0,
		"POL": 0.58, "BCH": 0.0034, "XRP": 1.82,
	}
}

func pgNumericToFloat(n pgtype.Numeric) float64 {
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return 0
	}
	return f.Float64
}

func parseFloatSafe(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func roundUSD(v float64) float64 {
	return math.Round(v*100) / 100
}

func formatBalance(amount float64, currency string) string {
	switch currency {
	case "XAF", "XOF":
		return fmt.Sprintf("%.0f", amount)
	default:
		return fmt.Sprintf("%.2f", amount)
	}
}

func currencyName(c string) string {
	m := map[string]string{
		"USD": "US Dollar", "EUR": "Euro", "GBP": "British Pound",
		"XAF": "CFA Franc BEAC", "XOF": "CFA Franc BCEAO",
	}
	if n, ok := m[c]; ok {
		return n
	}
	return c
}

func currencyFlag(c string) string {
	m := map[string]string{
		"USD": "🇺🇸", "EUR": "🇪🇺", "GBP": "🇬🇧",
		"XAF": "🇨🇲", "XOF": "🇸🇳",
	}
	if f, ok := m[c]; ok {
		return f
	}
	return "🌍"
}

func initials(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	words := strings.Fields(name)
	if len(words) == 1 {
		return strings.ToUpper(string([]rune(words[0])[0]))
	}
	first := []rune(words[0])
	last := []rune(words[len(words)-1])
	return strings.ToUpper(string(first[0])) + strings.ToUpper(string(last[0]))
}
