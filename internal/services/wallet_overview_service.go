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
//
// Type says WHAT it is; Provider says WHICH Rach rail powers it — this is what
// keeps the two very different "stablecoins" apart:
//   - Provider "caas"  → Instant USD (iUSD), an EIP-4337 SCW (Rach CaaS). Type "stablecoin".
//   - Provider "waas"  → on-chain crypto AND on-chain stablecoins (USDT/USDC per
//     chain) held in a Rach WaaS HD wallet. Type "crypto" or "stablecoin".
//   - Provider "hub2"/"nilos" → fiat accounts. Type "fiat".
type WalletItem struct {
	ID               string  `json:"id"`
	Symbol           string  `json:"symbol"`
	Name             string  `json:"name"`
	Type             string  `json:"type"`               // "fiat" | "stablecoin" | "crypto"
	Provider         string  `json:"provider,omitempty"` // "caas" | "waas" | "hub2" | "nilos"
	Balance          string  `json:"balance"`            // decimal string
	BalanceRaw       float64 `json:"balance_raw"`
	FormattedBalance string  `json:"formatted_balance"`         // "$12,450.75" | "0.4523 BTC"
	CurrencySymbol   string  `json:"currency_symbol,omitempty"` // "$" "€" "£"
	Flag             string  `json:"flag,omitempty"`
	Address          string  `json:"address,omitempty"`        // crypto / stablecoin receive address
	Network          string  `json:"network,omitempty"`        // "BTC" "ETH" etc.
	AccountNumber    string  `json:"account_number,omitempty"` // fiat account number
	BalanceUSD       float64 `json:"balance_usd"`
	HasWallet        bool    `json:"has_wallet"` // false = not yet provisioned
}

// PhoneSendCard is the "Phone Send — Instant · iUSD-powered" card at the top.
// Phone Send moves Instant USD (iUSD) over the CaaS rail.
type PhoneSendCard struct {
	Balance          float64       `json:"balance"`
	BalanceFormatted string        `json:"balance_formatted"` // "15,000.00 iUSD"
	Token            string        `json:"token"`             // "iUSD"
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
//
// For a WaaS crypto wallet, Wallet is the native coin and Tokens lists the
// on-chain stablecoins (USDT/USDC/…) held on that same address — so opening
// e.g. the Polygon wallet shows POL plus its USDT/USDC together.
type WalletDetailResponse struct {
	Wallet  WalletItem    `json:"wallet"`
	Tokens  []WalletItem  `json:"tokens,omitempty"`
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
	Amount          string    `json:"amount"` // "+$500.00" | "-$500.00"
	AmountRaw       float64   `json:"amount_raw"`
	Currency        string    `json:"currency"`
	Status          string    `json:"status"`
	Reference       string    `json:"reference"`
	ConvertedAmount *string   `json:"converted_amount,omitempty"` // "→ $1,080.00"
	Period          string    `json:"period"`                     // "this_week" | "last_week" | "earlier"
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
	Type      string `json:"type"`               // "crypto" | "stablecoin"
	Provider  string `json:"provider,omitempty"` // "caas" | "waas"
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
				Provider:         fiatProvider(acc.Currency),
				Balance:          formatBalance(bal, acc.Currency),
				BalanceRaw:       bal,
				FormattedBalance: fiatFormatted(bal, acc.Currency),
				CurrencySymbol:   currencySymbol(acc.Currency),
				Flag:             currencyFlag(acc.Currency),
				AccountNumber:    mobileMoneyAccountNumber(acc.Currency, acc.AccountNumber, user.PhoneNumber),
				BalanceUSD:       roundUSD(balUSD),
				HasWallet:        true,
			})
		}
	}

	// ── Instant USD (CaaS) — settles on-chain as USDC, shown as iUSD ──────────
	var caasUSDC float64
	if walletType == "" || walletType == "stablecoin" {
		if user.PhoneNumber != "" {
			var addr string
			if bal, err := s.caasClient.GetBalance(ctx, user.PhoneNumber); err == nil {
				caasUSDC = parseFloatSafe(bal.BalanceUSDC)
				addr = bal.WalletAddress
			} else {
				// Balance read can fail for a freshly-provisioned, unfunded SCW —
				// still show the iUSD wallet at 0 rather than hiding it.
				s.logger.Warn("wallet overview: caas balance unavailable, showing 0 iUSD", zap.Error(err))
			}
			totalUSD += caasUSDC
			wallets = append(wallets, WalletItem{
				Symbol:           IUSDSymbol,
				Name:             IUSDName,
				Type:             "stablecoin",
				Provider:         "caas",
				Balance:          fmt.Sprintf("%.2f", caasUSDC),
				BalanceRaw:       caasUSDC,
				FormattedBalance: fmt.Sprintf("%s %s", formatNumber(caasUSDC, 2), IUSDSymbol),
				Address:          addr,
				BalanceUSD:       roundUSD(caasUSDC),
				HasWallet:        true,
			})
		}
	}

	// ── Crypto + on-chain stablecoins (WaaS) ─────────────────────────────────
	// One WaaS HD address per chain can hold the native coin (type "crypto")
	// AND on-chain stablecoins like USDT/USDC (type "stablecoin", provider
	// "waas") — the latter are DISTINCT from CaaS iUSD above.
	if walletType == "" || walletType == "crypto" || walletType == "stablecoin" {
		waasWallets, err := q.GetWaasWalletsByUserID(ctx, userID)
		if err != nil {
			s.logger.Warn("wallet overview: could not load waas wallets", zap.Error(err))
		}

		// Try to fetch live balances for all wallets in one call.
		addrBalances := s.fetchWaaSBalances(ctx, user.ID.String())

		for _, w := range waasWallets {
			net := strings.ToUpper(w.Network)
			nb := addrBalances[net]

			// Native coin.
			if walletType == "" || walletType == "crypto" {
				balUSD := nb.Native / rates[net]
				totalUSD += balUSD
				wallets = append(wallets, WalletItem{
					ID:               w.ID.String(),
					Symbol:           net,
					Name:             cryptoName(w.Network),
					Type:             "crypto",
					Provider:         "waas",
					Balance:          fmt.Sprintf("%.8f", nb.Native),
					BalanceRaw:       nb.Native,
					FormattedBalance: formatCrypto(nb.Native, w.Network),
					Address:          w.Address,
					Network:          net,
					BalanceUSD:       roundUSD(balUSD),
					HasWallet:        true,
				})
			}

			// On-chain stablecoins on this same address (≈ $1 each).
			if walletType == "" || walletType == "stablecoin" {
				for _, tok := range nb.Tokens {
					totalUSD += tok.Balance
					wallets = append(wallets, WalletItem{
						ID:               w.ID.String() + ":" + tok.Currency,
						Symbol:           tok.Currency,
						Name:             fmt.Sprintf("%s on %s", stablecoinName(tok.Currency), cryptoName(w.Network)),
						Type:             "stablecoin",
						Provider:         "waas",
						Balance:          fmt.Sprintf("%.2f", tok.Balance),
						BalanceRaw:       tok.Balance,
						FormattedBalance: fmt.Sprintf("%s %s", formatNumber(tok.Balance, 2), tok.Currency),
						Address:          w.Address,
						Network:          net,
						BalanceUSD:       roundUSD(tok.Balance),
						HasWallet:        true,
					})
				}
			}
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
		BalanceFormatted: fmt.Sprintf("%s %s", formatNumber(caasUSDC, 2), IUSDSymbol),
		Token:            IUSDSymbol,
		RecentContacts:   contacts,
	}

	return &WalletsOverview{
		PhoneSend:         phoneSend,
		Wallets:           wallets,
		TotalUSD:          roundUSD(totalUSD),
		SupportedNetworks: supportedNetworks(),
	}, nil
}

// waasToken is a single on-chain token balance held in a WaaS wallet.
type waasToken struct {
	Currency string
	Balance  float64
}

// waasNetworkBalance is a WaaS address's holdings on one chain: the native coin
// plus any on-chain stablecoins (USDT/USDC/…) sitting on that same address.
type waasNetworkBalance struct {
	Native float64
	Tokens []waasToken
}

// isNativeCurrency reports whether a balance currency is the native coin of a
// WaaS network. The payments backend labels the native balance with the NETWORK
// CODE (getNativeCurrencyName → "POL", "BSC", "ETH", …), so `currency == network`
// is the real rule. The alias cases are defensive safety nets in case the native
// coin ever arrives under its ticker instead:
//   - POL network: also accept "MATIC" — Polygon renamed MATIC → POL, same token,
//     so we must never drop it.
//   - BSC network: also accept "BNB".
func isNativeCurrency(network, currency string) bool {
	network = strings.ToUpper(network)
	currency = strings.ToUpper(currency)
	if currency == network {
		return true
	}
	switch network {
	case "POL":
		return currency == "MATIC" // POL == MATIC (rebrand)
	case "BSC":
		return currency == "BNB"
	}
	return false
}

// waasStablecoins are the on-chain stablecoins WaaS can custody per chain. These
// are DISTINCT from CaaS iUSD — they live in the WaaS HD wallet, not the SCW.
var waasStablecoins = map[string]bool{"USDT": true, "USDC": true, "DAI": true, "BUSD": true}

// fetchWaaSBalances calls ListCustomerAddresses and returns, per network, the
// native-coin balance plus any on-chain stablecoin balances. On any error it
// returns an empty map (callers get zero balances rather than failing).
func (s *WalletOverviewService) fetchWaaSBalances(ctx context.Context, customerID string) map[string]waasNetworkBalance {
	out := map[string]waasNetworkBalance{}
	resp, err := s.paymentsClient.ListCustomerAddresses(ctx, customerID, false)
	if err != nil {
		s.logger.Warn("waas: could not fetch address balances", zap.Error(err))
		return out
	}

	// Matches the payments AddressBalance model exactly (address_balances table):
	// each address carries total_received (cumulative) and a balances[] array of
	// { currency, balance } rows. currency is the network code for the native
	// coin (e.g. "POL", "BSC") or the token symbol for stablecoins ("USDT",
	// "USDC"); balance is a decimal string. There is no top-level current balance.
	type addrPayload struct {
		Network       string `json:"network"`
		TotalReceived string `json:"total_received"`
		Balances      []struct {
			Currency string `json:"currency"`
			Balance  string `json:"balance"`
		} `json:"balances"`
	}

	for _, raw := range resp.Addresses {
		var addr addrPayload
		if err := json.Unmarshal(raw, &addr); err != nil {
			continue
		}
		net := strings.ToUpper(addr.Network)
		nb := out[net]

		// Classify each balance as native coin vs on-chain stablecoin. Native is
		// summed (never overwritten) so a POL/MATIC alias split is never lost.
		for _, b := range addr.Balances {
			cur := strings.ToUpper(strings.TrimSpace(b.Currency))
			f, ferr := strconv.ParseFloat(strings.TrimSpace(b.Balance), 64)
			if ferr != nil || cur == "" {
				continue
			}
			switch {
			case isNativeCurrency(net, cur):
				nb.Native += f
			case waasStablecoins[cur]:
				nb.Tokens = append(nb.Tokens, waasToken{Currency: cur, Balance: f})
			}
		}
		out[net] = nb
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

	// For HUB2 mobile-money currencies the identifier is the user's phone.
	acctNumber := acc.AccountNumber
	if IsMobileMoneyCurrency(acc.Currency) {
		if u, uerr := q.GetUserByID(ctx, userID); uerr == nil {
			acctNumber = mobileMoneyAccountNumber(acc.Currency, acc.AccountNumber, u.PhoneNumber)
		}
	}

	return &WalletDetailResponse{
		Wallet: WalletItem{
			ID:               acc.ID.String(),
			Symbol:           acc.Currency,
			Name:             currencyName(acc.Currency),
			Type:             "fiat",
			Provider:         fiatProvider(acc.Currency),
			Balance:          formatBalance(bal, acc.Currency),
			BalanceRaw:       bal,
			FormattedBalance: fiatFormatted(bal, acc.Currency),
			CurrencySymbol:   currencySymbol(acc.Currency),
			Flag:             currencyFlag(acc.Currency),
			AccountNumber:    acctNumber,
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

	// Try live balances from Payments API (WaaS): native coin + on-chain tokens.
	net := strings.ToUpper(wallet.Network)
	nb := s.fetchWaaSBalances(ctx, userID.String())[net]
	bal := nb.Native

	// On-chain stablecoins (USDT/USDC/…) sitting on this same WaaS address.
	tokens := make([]WalletItem, 0, len(nb.Tokens))
	for _, tok := range nb.Tokens {
		tokens = append(tokens, WalletItem{
			ID:               wallet.ID.String() + ":" + tok.Currency,
			Symbol:           tok.Currency,
			Name:             fmt.Sprintf("%s on %s", stablecoinName(tok.Currency), cryptoName(wallet.Network)),
			Type:             "stablecoin",
			Provider:         "waas",
			Balance:          fmt.Sprintf("%.2f", tok.Balance),
			BalanceRaw:       tok.Balance,
			FormattedBalance: fmt.Sprintf("%s %s", formatNumber(tok.Balance, 2), tok.Currency),
			Address:          wallet.Address,
			Network:          net,
			BalanceUSD:       roundUSD(tok.Balance),
			HasWallet:        true,
		})
	}

	return &WalletDetailResponse{
		Wallet: WalletItem{
			ID:               wallet.ID.String(),
			Symbol:           net,
			Name:             cryptoName(wallet.Network),
			Type:             "crypto",
			Provider:         "waas",
			Balance:          fmt.Sprintf("%.8f", bal),
			BalanceRaw:       bal,
			FormattedBalance: formatCrypto(bal, wallet.Network),
			Address:          wallet.Address,
			Network:          net,
			BalanceUSD:       roundUSD(bal / defaultFXRates()[net]),
			HasWallet:        true,
		},
		Tokens:  tokens,
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

	// iUSD (formerly shown as USDC) is the only supported CaaS stablecoin.
	sym := strings.ToUpper(symbol)
	if sym != "IUSD" && sym != "USDC" {
		return nil, fmt.Errorf("unsupported stablecoin: %s", symbol)
	}

	// Balance read is best-effort — a freshly-provisioned, unfunded SCW should
	// still show iUSD 0 rather than erroring.
	var balFloat float64
	var addr string
	if bal, berr := s.caasClient.GetBalance(ctx, user.PhoneNumber); berr == nil {
		balFloat = parseFloatSafe(bal.BalanceUSDC)
		addr = bal.WalletAddress
	} else {
		s.logger.Warn("stablecoin detail: caas balance unavailable, showing 0 iUSD", zap.Error(berr))
	}

	return &WalletDetailResponse{
		Wallet: WalletItem{
			Symbol:           IUSDSymbol,
			Name:             IUSDName,
			Type:             "stablecoin",
			Provider:         "caas",
			Balance:          fmt.Sprintf("%.2f", balFloat),
			BalanceRaw:       balFloat,
			FormattedBalance: fmt.Sprintf("%s %s", formatNumber(balFloat, 2), IUSDSymbol),
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
		// Rach WaaS — native coins (each chain's HD address can also hold
		// on-chain USDT/USDC once funded).
		{Symbol: "BTC", Name: "Bitcoin", Network: "BTC", Type: "crypto", Provider: "waas"},
		{Symbol: "ETH", Name: "Ethereum", Network: "ETH", Type: "crypto", Provider: "waas"},
		{Symbol: "BSC", Name: "BNB Smart Chain", Network: "BSC", Type: "crypto", Provider: "waas"},
		{Symbol: "SOL", Name: "Solana", Network: "SOL", Type: "crypto", Provider: "waas"},
		{Symbol: "LTC", Name: "Litecoin", Network: "LTC", Type: "crypto", Provider: "waas"},
		{Symbol: "TRX", Name: "TRON", Network: "TRX", Type: "crypto", Provider: "waas"},
		{Symbol: "POL", Name: "Polygon", Network: "POL", Type: "crypto", Provider: "waas"},
		{Symbol: "BCH", Name: "Bitcoin Cash", Network: "BCH", Type: "crypto", Provider: "waas"},
		{Symbol: "XRP", Name: "XRP", Network: "XRP", Type: "crypto", Provider: "waas"},
		// Rach CaaS — Instant USD (EIP-4337 SCW), a different rail from WaaS.
		{Symbol: IUSDSymbol, Name: IUSDName, Network: "EIP-4337", Type: "stablecoin", Provider: "caas"},
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

// supportedNetworks lists the Rach WaaS networks (spec enum:
// BTC BCH LTC ETH BSC POL TRX SOL XRP).
func supportedNetworks() []string {
	return []string{"BTC", "BCH", "LTC", "ETH", "BSC", "POL", "TRX", "SOL", "XRP"}
}

// fiatProvider reports which rail settles a fiat currency: HUB2 for the CFA
// mobile-money currencies, Nilos for everything else.
func fiatProvider(currency string) string {
	if IsMobileMoneyCurrency(currency) {
		return "hub2"
	}
	return "nilos"
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
