package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/caas"
	"github.com/rachfinance/digitalfx/internal/clients/payments"
	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

// ─── Response types ───────────────────────────────────────────────────────────

// WalletItem is a single entry in the unified wallet list (fiat / stablecoin / crypto).
type WalletItem struct {
	ID               string  `json:"id"`
	Symbol           string  `json:"symbol"`
	Name             string  `json:"name"`
	Type             string  `json:"type"`             // "fiat" | "stablecoin" | "crypto"
	Balance          string  `json:"balance"`          // decimal string
	BalanceRaw       float64 `json:"balance_raw"`
	FormattedBalance string  `json:"formatted_balance"` // "$12,450.75" | "0.4523 BTC"
	CurrencySymbol   string  `json:"currency_symbol,omitempty"` // "$" "€" "£"
	Flag             string  `json:"flag,omitempty"`
	Address          string  `json:"address,omitempty"`       // crypto / stablecoin receive address
	Network          string  `json:"network,omitempty"`       // "BTC" "ETH" etc.
	AccountNumber    string  `json:"account_number,omitempty"` // fiat account number
	BalanceUSD       float64 `json:"balance_usd"`
	HasWallet        bool    `json:"has_wallet"` // false = not yet provisioned
}

// PhoneSendCard is the "Phone Send — Instant · Stablecoin-powered" card at the top.
type PhoneSendCard struct {
	Balance          float64       `json:"balance"`
	BalanceFormatted string        `json:"balance_formatted"` // "$15,000.00 USDC"
	Token            string        `json:"token"`             // "USDC"
	RecentContacts   []ContactItem `json:"recent_contacts"`
}

// WalletsOverview is the full wallets screen payload.
type WalletsOverview struct {
	PhoneSend         PhoneSendCard `json:"phone_send"`
	Wallets           []WalletItem  `json:"wallets"`
	TotalUSD          float64       `json:"total_usd"`
	SupportedNetworks []string      `json:"supported_networks"`
}

// WalletDetailResponse is the wallet detail screen header.
type WalletDetailResponse struct {
	Wallet  WalletItem    `json:"wallet"`
	Actions WalletActions `json:"actions"`
}

// WalletActions describes what the user can do from the detail screen.
type WalletActions struct {
	CanSend     bool `json:"can_send"`
	CanReceive  bool `json:"can_receive"`
	CanExchange bool `json:"can_exchange"`
	CanWithdraw bool `json:"can_withdraw"`
}

// WalletTxItem is one row in the transaction list, enriched for display.
type WalletTxItem struct {
	ID              string    `json:"id"`
	Type            string    `json:"type"`      // "sent"|"received"|"exchanged"|"deposited"|"withdrawn"
	Direction       string    `json:"direction"` // "in" | "out"
	Label           string    `json:"label"`     // "Sent", "Received", "Exchanged EUR → USD"
	Description     string    `json:"description"`
	Amount          string    `json:"amount"`     // "+$500.00" | "-$500.00"
	AmountRaw       float64   `json:"amount_raw"`
	Currency        string    `json:"currency"`
	Status          string    `json:"status"`
	Reference       string    `json:"reference"`
	ConvertedAmount *string   `json:"converted_amount,omitempty"` // "→ $1,080.00"
	Period          string    `json:"period"` // "this_week" | "last_week" | "earlier"
	CreatedAt       time.Time `json:"created_at"`
}

// WalletTxGroup groups transactions by time period (THIS WEEK / LAST WEEK / EARLIER).
type WalletTxGroup struct {
	Period string         `json:"period"`
	Label  string         `json:"label"` // "THIS WEEK"
	Count  int            `json:"count"`
	Items  []WalletTxItem `json:"items"`
}

// WalletTxStats is the summary banner (In / Out / Total count).
type WalletTxStats struct {
	TotalInRaw  float64 `json:"total_in_raw"`
	TotalOutRaw float64 `json:"total_out_raw"`
	TotalIn     string  `json:"total_in"`
	TotalOut    string  `json:"total_out"`
	Count       int64   `json:"count"`
}

// WalletTransactionsResult is the full transaction history response for one wallet.
type WalletTransactionsResult struct {
	Stats  WalletTxStats   `json:"stats"`
	Groups []WalletTxGroup `json:"groups"`
	Total  int64           `json:"total"`
	Page   int32           `json:"page"`
	Limit  int32           `json:"limit"`
}

// SupportedAsset is returned by GetSupportedAssets for the + Add flow.
type SupportedAsset struct {
	Symbol    string `json:"symbol"`
	Name      string `json:"name"`
	Network   string `json:"network"`
	Type      string `json:"type"` // "crypto" | "stablecoin"
	HasWallet bool   `json:"has_wallet"`
	Address   string `json:"address,omitempty"`
}

// ─── Service ──────────────────────────────────────────────────────────────────

type WalletOverviewService struct {
	pool           *pgxpool.Pool
	caasClient     *caas.Client
	paymentsClient *payments.Client
	logger         *zap.Logger
}

func NewWalletOverviewService(
	pool *pgxpool.Pool,
	caasClient *caas.Client,
	paymentsClient *payments.Client,
	logger *zap.Logger,
) *WalletOverviewService {
	return &WalletOverviewService{pool: pool, caasClient: caasClient, paymentsClient: paymentsClient, logger: logger}
}

// ─── Wallets Overview ─────────────────────────────────────────────────────────

// GetOverview builds the full wallet list: fiat + stablecoins + crypto.
// walletType filters: "" | "fiat" | "stablecoin" | "crypto"
func (s *WalletOverviewService) GetOverview(ctx context.Context, userID uuid.UUID, walletType string) (*WalletsOverview, error) {
	q := db.New(s.pool)
	rates := defaultFXRates()

	user, _ := q.GetUserByID(ctx, userID)

	var totalUSD float64
	var wallets []WalletItem

	// ── Fiat accounts ────────────────────────────────────────────────────────
	if walletType == "" || walletType == "fiat" {
		accounts, err := q.GetAccountsByUserID(ctx, userID)
		if err != nil {
			s.logger.Warn("wallet overview: could not load fiat accounts", zap.Error(err))
		}
		for _, acc := range accounts {
			bal := pgNumericToFloat(acc.Balance)
			balUSD := bal / rates[acc.Currency]
			totalUSD += balUSD
			wallets = append(wallets, WalletItem{
				ID:               acc.ID.String(),
				Symbol:           acc.Currency,
				Name:             currencyName(acc.Currency),
				Type:             "fiat",
				Balance:          formatBalance(bal, acc.Currency),
				BalanceRaw:       bal,
				FormattedBalance: fiatFormatted(bal, acc.Currency),
				CurrencySymbol:   currencySymbol(acc.Currency),
				Flag:             currencyFlag(acc.Currency),
				AccountNumber:    acc.AccountNumber,
				BalanceUSD:       roundUSD(balUSD),
				HasWallet:        true,
			})
		}
	}

	// ── Stablecoins (CaaS) ───────────────────────────────────────────────────
	var caasUSDC float64
	if walletType == "" || walletType == "stablecoin" {
		if user.PhoneNumber != "" {
			if bal, err := s.caasClient.GetBalance(ctx, user.PhoneNumber); err == nil {
				caasUSDC = parseFloatSafe(bal.BalanceUSDC)
				totalUSD += caasUSDC
				wallets = append(wallets, WalletItem{
					Symbol:           "USDC",
					Name:             "USD Coin",
					Type:             "stablecoin",
					Balance:          fmt.Sprintf("%.2f", caasUSDC),
					BalanceRaw:       caasUSDC,
					FormattedBalance: fmt.Sprintf("%s USDC", formatNumber(caasUSDC, 2)),
					Address:          bal.WalletAddress,
					BalanceUSD:       roundUSD(caasUSDC),
					HasWallet:        true,
				})
			} else {
				s.logger.Warn("wallet overview: caas balance unavailable", zap.Error(err))
			}
		}
	}

	// ── Crypto wallets (WaaS) ────────────────────────────────────────────────
	if walletType == "" || walletType == "crypto" {
		waasWallets, err := q.GetWaasWalletsByUserID(ctx, userID)
		if err != nil {
			s.logger.Warn("wallet overview: could not load waas wallets", zap.Error(err))
		}

		// Try to fetch live balances for all wallets in one call.
		addrBalances := s.fetchWaaSBalances(ctx, user.ID.String())

		for _, w := range waasWallets {
			bal := addrBalances[strings.ToUpper(w.Network)]
			balUSD := bal / rates[strings.ToUpper(w.Network)]
			totalUSD += balUSD
			wallets = append(wallets, WalletItem{
				ID:               w.ID.String(),
				Symbol:           strings.ToUpper(w.Network),
				Name:             cryptoName(w.Network),
				Type:             "crypto",
				Balance:          fmt.Sprintf("%.8f", bal),
				BalanceRaw:       bal,
				FormattedBalance: formatCrypto(bal, w.Network),
				Address:          w.Address,
				Network:          strings.ToUpper(w.Network),
				BalanceUSD:       roundUSD(balUSD),
				HasWallet:        true,
			})
		}
	}

	// ── Recent contacts (for Phone Send card) ────────────────────────────────
	dbContacts, _ := q.GetRecentContacts(ctx, userID, 5)
	contacts := make([]ContactItem, 0, len(dbContacts))
	for _, c := range dbContacts {
		contacts = append(contacts, ContactItem{Name: c.Name, PhoneNumber: c.PhoneNumber, Initials: initials(c.Name)})
	}

	phoneSend := PhoneSendCard{
		Balance:          caasUSDC,
		BalanceFormatted: fmt.Sprintf("$%s USDC", formatNumber(caasUSDC, 2)),
		Token:            "USDC",
		RecentContacts:   contacts,
	}

	return &WalletsOverview{
		PhoneSend:         phoneSend,
		Wallets:           wallets,
		TotalUSD:          roundUSD(totalUSD),
		SupportedNetworks: supportedNetworks(),
	}, nil
}

// fetchWaaSBalances calls ListCustomerAddresses and returns network → balance map.
// On any error, returns an empty map (callers get zero balances rather than failing).
func (s *WalletOverviewService) fetchWaaSBalances(ctx context.Context, customerID string) map[string]float64 {
	out := map[string]float64{}
	resp, err := s.paymentsClient.ListCustomerAddresses(ctx, customerID, false)
	if err != nil {
		s.logger.Warn("waas: could not fetch address balances", zap.Error(err))
		return out
	}

	type addrPayload struct {
		Network  string `json:"network"`
		Balance  string `json:"balance"`
		Balances []struct {
			Currency string `json:"currency"`
			Balance  string `json:"balance"`
		} `json:"balances"`
	}

	for _, raw := range resp.Addresses {
		var addr addrPayload
		if err := json.Unmarshal(raw, &addr); err != nil {
			continue
		}
		network := strings.ToUpper(addr.Network)
		// Prefer top-level balance if present, else first entry in Balances[].
		balStr := addr.Balance
		if balStr == "" && len(addr.Balances) > 0 {
			balStr = addr.Balances[0].Balance
		}
		if f, err := strconv.ParseFloat(balStr, 64); err == nil {
			out[network] = f
		}
	}
	return out
}

// ─── Fiat Wallet Detail ───────────────────────────────────────────────────────

func (s *WalletOverviewService) GetFiatWalletDetail(ctx context.Context, userID uuid.UUID, currency string) (*WalletDetailResponse, error) {
	q := db.New(s.pool)
	acc, err := q.GetAccountByUserAndCurrency(ctx, db.GetAccountByUserAndCurrencyParams{UserID: userID, Currency: strings.ToUpper(currency)})
	if err != nil {
		return nil, ErrAccountNotFound
	}
	bal := pgNumericToFloat(acc.Balance)

	return &WalletDetailResponse{
		Wallet: WalletItem{
			ID:               acc.ID.String(),
			Symbol:           acc.Currency,
			Name:             currencyName(acc.Currency),
			Type:             "fiat",
			Balance:          formatBalance(bal, acc.Currency),
			BalanceRaw:       bal,
			FormattedBalance: fiatFormatted(bal, acc.Currency),
			CurrencySymbol:   currencySymbol(acc.Currency),
			Flag:             currencyFlag(acc.Currency),
			AccountNumber:    acc.AccountNumber,
			BalanceUSD:       roundUSD(bal / defaultFXRates()[acc.Currency]),
			HasWallet:        true,
		},
		Actions: WalletActions{CanSend: true, CanReceive: true, CanExchange: true, CanWithdraw: true},
	}, nil
}

// GetFiatTransactions returns the wallet detail transaction history with grouping.
// typeFilter: "" | "sent" | "received" | "exchanged" | "deposited" | "withdrawn"
func (s *WalletOverviewService) GetFiatTransactions(
	ctx context.Context,
	userID uuid.UUID,
	currency, typeFilter, search string,
	page, limit int32,
) (*WalletTransactionsResult, error) {
	q := db.New(s.pool)

	acc, err := q.GetAccountByUserAndCurrency(ctx, db.GetAccountByUserAndCurrencyParams{UserID: userID, Currency: strings.ToUpper(currency)})
	if err != nil {
		return nil, ErrAccountNotFound
	}

	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	dbFilter := uiTypeToDBType(typeFilter)
	params := db.ListWalletTransactionsParams{
		AccountID:  acc.ID,
		TypeFilter: dbFilter,
		Search:     search,
		Limit:      limit,
		Offset:     offset,
	}

	txns, _ := q.ListWalletTransactions(ctx, params)
	total, _ := q.CountWalletTransactions(ctx, params)
	stats, _ := q.GetWalletTxStats(ctx, acc.ID)

	items := make([]WalletTxItem, 0, len(txns))
	for _, t := range txns {
		items = append(items, mapTransaction(t, acc.Currency))
	}

	groups := groupByPeriod(items)

	formattedIn := fmt.Sprintf("%s%s", currencySymbol(acc.Currency), formatNumber(stats.TotalIn, 2))
	formattedOut := fmt.Sprintf("%s%s", currencySymbol(acc.Currency), formatNumber(stats.TotalOut, 2))
	if strings.Contains(acc.Currency, "XA") || strings.Contains(acc.Currency, "XO") {
		formattedIn = formatNumber(stats.TotalIn, 0) + " " + acc.Currency
		formattedOut = formatNumber(stats.TotalOut, 0) + " " + acc.Currency
	}

	return &WalletTransactionsResult{
		Stats: WalletTxStats{
			TotalInRaw:  stats.TotalIn,
			TotalOutRaw: stats.TotalOut,
			TotalIn:     formattedIn,
			TotalOut:    formattedOut,
			Count:       stats.Count,
		},
		Groups: groups,
		Total:  total,
		Page:   page,
		Limit:  limit,
	}, nil
}

// ─── Crypto Wallet Detail ─────────────────────────────────────────────────────

func (s *WalletOverviewService) GetCryptoWalletDetail(ctx context.Context, userID uuid.UUID, network string) (*WalletDetailResponse, error) {
	q := db.New(s.pool)
	wallet, err := q.GetWaasWalletByNetwork(ctx, db.GetWaasWalletByNetworkParams{
		UserID:  userID,
		Network: strings.ToUpper(network),
	})
	if err != nil {
		return nil, ErrWalletNotFound
	}

	// Try live balance from Payments API.
	balMap := s.fetchWaaSBalances(ctx, userID.String())
	bal := balMap[strings.ToUpper(network)]

	return &WalletDetailResponse{
		Wallet: WalletItem{
			ID:               wallet.ID.String(),
			Symbol:           strings.ToUpper(wallet.Network),
			Name:             cryptoName(wallet.Network),
			Type:             "crypto",
			Balance:          fmt.Sprintf("%.8f", bal),
			BalanceRaw:       bal,
			FormattedBalance: formatCrypto(bal, wallet.Network),
			Address:          wallet.Address,
			Network:          strings.ToUpper(wallet.Network),
			BalanceUSD:       roundUSD(bal / defaultFXRates()[strings.ToUpper(wallet.Network)]),
			HasWallet:        true,
		},
		Actions: WalletActions{CanSend: true, CanReceive: true, CanExchange: false, CanWithdraw: false},
	}, nil
}

// GetCryptoTransactions fetches on-chain transaction history from the Payments API.
func (s *WalletOverviewService) GetCryptoTransactions(ctx context.Context, userID uuid.UUID, network string, page, limit int32) (map[string]any, error) {
	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}
	resp, err := s.paymentsClient.GetTransactions(ctx, userID.String(), payments.GetTransactionsParams{
		Network: payments.Network(strings.ToUpper(network)),
		Page:    int(page),
		Limit:   int(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("payments api transactions: %w", err)
	}
	return map[string]any{
		"transactions": resp.Transactions,
		"total":        resp.Total,
		"page":         resp.Page,
		"limit":        resp.Limit,
		"network":      strings.ToUpper(network),
	}, nil
}

// ─── Stablecoin Wallet Detail ─────────────────────────────────────────────────

func (s *WalletOverviewService) GetStablecoinDetail(ctx context.Context, userID uuid.UUID, symbol string) (*WalletDetailResponse, error) {
	q := db.New(s.pool)
	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	bal, err := s.caasClient.GetBalance(ctx, user.PhoneNumber)
	if err != nil {
		return nil, fmt.Errorf("caas balance: %w", err)
	}

	sym := strings.ToUpper(symbol)
	var balFloat float64
	var addr string
	switch sym {
	case "USDC":
		balFloat = parseFloatSafe(bal.BalanceUSDC)
		addr = bal.WalletAddress
	default:
		return nil, fmt.Errorf("unsupported stablecoin: %s", sym)
	}

	return &WalletDetailResponse{
		Wallet: WalletItem{
			Symbol:           sym,
			Name:             stablecoinName(sym),
			Type:             "stablecoin",
			Balance:          fmt.Sprintf("%.2f", balFloat),
			BalanceRaw:       balFloat,
			FormattedBalance: fmt.Sprintf("%s %s", formatNumber(balFloat, 2), sym),
			Address:          addr,
			BalanceUSD:       roundUSD(balFloat),
			HasWallet:        true,
		},
		Actions: WalletActions{CanSend: true, CanReceive: true, CanExchange: false, CanWithdraw: true},
	}, nil
}

// ─── Supported Assets (for + Add) ────────────────────────────────────────────

func (s *WalletOverviewService) GetSupportedAssets(ctx context.Context, userID uuid.UUID) ([]SupportedAsset, error) {
	q := db.New(s.pool)
	existing, _ := q.GetWaasWalletsByUserID(ctx, userID)

	hasNetwork := map[string]string{}
	for _, w := range existing {
		hasNetwork[strings.ToUpper(w.Network)] = w.Address
	}

	assets := []SupportedAsset{
		{Symbol: "BTC", Name: "Bitcoin", Network: "BTC", Type: "crypto"},
		{Symbol: "ETH", Name: "Ethereum", Network: "ETH", Type: "crypto"},
		{Symbol: "SOL", Name: "Solana", Network: "SOL", Type: "crypto"},
		{Symbol: "LTC", Name: "Litecoin", Network: "LTC", Type: "crypto"},
		{Symbol: "TRX", Name: "TRON", Network: "TRX", Type: "crypto"},
		{Symbol: "POL", Name: "Polygon", Network: "POL", Type: "crypto"},
		{Symbol: "BCH", Name: "Bitcoin Cash", Network: "BCH", Type: "crypto"},
		{Symbol: "XRP", Name: "XRP", Network: "XRP", Type: "crypto"},
		{Symbol: "USDC", Name: "USD Coin", Network: "ERC-20", Type: "stablecoin"},
	}

	for i, a := range assets {
		if addr, ok := hasNetwork[a.Network]; ok {
			assets[i].HasWallet = true
			assets[i].Address = addr
		}
	}

	return assets, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func supportedNetworks() []string {
	return []string{"BTC", "ETH", "SOL", "LTC", "TRX", "POL", "BCH", "XRP"}
}

func currencySymbol(c string) string {
	m := map[string]string{"USD": "$", "EUR": "€", "GBP": "£", "XAF": "", "XOF": ""}
	if s, ok := m[c]; ok {
		return s
	}
	return ""
}

func fiatFormatted(amount float64, currency string) string {
	sym := currencySymbol(currency)
	formatted := formatNumber(amount, 2)
	if strings.Contains(currency, "XA") || strings.Contains(currency, "XO") {
		formatted = formatNumber(amount, 0)
		return fmt.Sprintf("%s %s", formatted, currency)
	}
	return sym + formatted
}

func formatNumber(f float64, decimals int) string {
	format := fmt.Sprintf("%%.%df", decimals)
	s := fmt.Sprintf(format, f)
	// Insert thousands separators.
	parts := strings.Split(s, ".")
	intPart := parts[0]
	result := ""
	for i, ch := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 && intPart[0] != '-' {
			result += ","
		}
		result += string(ch)
	}
	if len(parts) > 1 {
		return result + "." + parts[1]
	}
	return result
}

func formatCrypto(amount float64, network string) string {
	sym := strings.ToUpper(network)
	switch sym {
	case "BTC", "LTC", "BCH":
		return fmt.Sprintf("%.4f %s", amount, sym)
	case "ETH":
		return fmt.Sprintf("%.3f %s", amount, sym)
	case "SOL":
		return fmt.Sprintf("%.2f %s", amount, sym)
	case "XRP":
		return fmt.Sprintf("%.4f %s", amount, sym)
	default:
		return fmt.Sprintf("%.2f %s", amount, sym)
	}
}

func cryptoName(network string) string {
	m := map[string]string{
		"BTC": "Bitcoin", "ETH": "Ethereum", "SOL": "Solana",
		"LTC": "Litecoin", "TRX": "TRON", "POL": "Polygon",
		"BCH": "Bitcoin Cash", "XRP": "XRP", "BSC": "BNB Smart Chain",
	}
	if n, ok := m[strings.ToUpper(network)]; ok {
		return n
	}
	return strings.ToUpper(network)
}

func stablecoinName(sym string) string {
	m := map[string]string{"USDC": "USD Coin", "USDT": "Tether"}
	if n, ok := m[sym]; ok {
		return n
	}
	return sym
}

// uiTypeToDBType maps the UI tab value to the DB transaction type field.
func uiTypeToDBType(uiType string) string {
	switch strings.ToLower(uiType) {
	case "sent":
		return "transfer_out"
	case "received":
		return "transfer_in"
	case "exchanged":
		return "exchange"
	case "deposited":
		return "deposit"
	case "withdrawn":
		return "withdrawal"
	default:
		return "" // all
	}
}

// mapTransaction converts a db.Transaction into a display-ready WalletTxItem.
func mapTransaction(t db.Transaction, currency string) WalletTxItem {
	direction, txType, label, description, converted := parseTxMeta(t)

	amount := pgNumericToFloat(t.Amount)
	sign := "+"
	if direction == "out" {
		sign = "-"
	}
	sym := currencySymbol(currency)
	amountStr := fmt.Sprintf("%s%s%s", sign, sym, formatNumber(amount, 2))
	if sym == "" {
		amountStr = fmt.Sprintf("%s%s %s", sign, formatNumber(amount, 0), currency)
	}

	desc := description
	if desc == "" && t.Description != nil {
		desc = *t.Description
	}

	period, _ := txPeriod(t.CreatedAt)

	return WalletTxItem{
		ID:              t.ID.String(),
		Type:            txType,
		Direction:       direction,
		Label:           label,
		Description:     desc,
		Amount:          amountStr,
		AmountRaw:       amount,
		Currency:        t.Currency,
		Status:          t.Status,
		Reference:       t.Reference,
		ConvertedAmount: converted,
		Period:          period,
		CreatedAt:       t.CreatedAt,
	}
}

// parseTxMeta extracts display fields from the transaction Type + Metadata.
func parseTxMeta(t db.Transaction) (direction, txType, label, description string, converted *string) {
	var meta map[string]any
	if len(t.Metadata) > 0 {
		_ = json.Unmarshal(t.Metadata, &meta)
	}

	metaStr := func(k string) string {
		if v, ok := meta[k]; ok {
			return fmt.Sprintf("%v", v)
		}
		return ""
	}

	switch t.Type {
	case "transfer_in", "credit":
		direction, txType, label = "in", "received", "Received"
		description = metaStr("sender")

	case "transfer_out", "debit":
		direction, txType, label = "out", "sent", "Sent"
		description = metaStr("recipient")
		if description == "" {
			description = metaStr("email")
		}

	case "exchange":
		direction, txType = "in", "exchanged"
		from := metaStr("from_currency")
		to := metaStr("to_currency")
		if from != "" && to != "" {
			label = fmt.Sprintf("Exchanged %s → %s", from, to)
		} else {
			label = "Exchanged"
		}
		if cv := metaStr("converted_amount"); cv != "" {
			s := fmt.Sprintf("→ %s", cv)
			converted = &s
		}

	case "deposit":
		direction, txType, label = "in", "deposited", "Deposited"
		description = metaStr("operator")

	case "withdrawal":
		direction, txType, label = "out", "withdrawn", "Withdrawn"
		description = metaStr("destination")

	default:
		direction, txType, label = "in", t.Type, strings.Title(t.Type)
	}
	return
}

// txPeriod classifies a timestamp into display period buckets.
func txPeriod(t time.Time) (string, string) {
	now := time.Now()
	startOfWeek := now.AddDate(0, 0, -int(now.Weekday()))
	startOfLastWeek := startOfWeek.AddDate(0, 0, -7)
	if t.After(startOfWeek) {
		return "this_week", "THIS WEEK"
	}
	if t.After(startOfLastWeek) {
		return "last_week", "LAST WEEK"
	}
	return "earlier", "EARLIER"
}

// groupByPeriod groups a flat list of WalletTxItems into period buckets.
func groupByPeriod(items []WalletTxItem) []WalletTxGroup {
	order := []string{"this_week", "last_week", "earlier"}
	labels := map[string]string{"this_week": "THIS WEEK", "last_week": "LAST WEEK", "earlier": "EARLIER"}
	buckets := map[string][]WalletTxItem{}
	for _, item := range items {
		buckets[item.Period] = append(buckets[item.Period], item)
	}
	var groups []WalletTxGroup
	for _, period := range order {
		if len(buckets[period]) > 0 {
			groups = append(groups, WalletTxGroup{
				Period: period,
				Label:  labels[period],
				Count:  len(buckets[period]),
				Items:  buckets[period],
			})
		}
	}
	return groups
}
