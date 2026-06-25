package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type WalletOverviewHandler struct {
	svc *services.Services
}

func NewWalletOverviewHandler(svc *services.Services) *WalletOverviewHandler {
	return &WalletOverviewHandler{svc: svc}
}

// ─── Wallets screen ───────────────────────────────────────────────────────────

// GetOverview godoc
//
//	@Summary      Wallets overview
//	@Description  Returns the Phone Send card (USDC balance + recent contacts) plus the unified wallet list (fiat + stablecoins + crypto). Filter with ?type=fiat|stablecoin|crypto.
//	@Tags         wallets
//	@Produce      json
//	@Security     BearerAuth
//	@Param        type  query   string  false  "Wallet type filter"  Enums(fiat, stablecoin, crypto)
//	@Success      200   {object}  map[string]any
//	@Failure      401   {object}  ErrorResponse
//	@Router       /wallets/overview [get]
func (h *WalletOverviewHandler) GetOverview(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	walletType := strings.ToLower(r.URL.Query().Get("type"))
	overview, err := h.svc.WalletOverview.GetOverview(r.Context(), userID, walletType)
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, overview)
}

// GetSupportedAssets godoc
//
//	@Summary      Supported assets
//	@Description  Returns all addable assets (crypto networks + stablecoins) with has_wallet=true if the user has already provisioned that wallet. Used for the + Add flow.
//	@Tags         wallets
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200   {array}   map[string]any
//	@Failure      401   {object}  ErrorResponse
//	@Router       /wallets/supported-assets [get]
func (h *WalletOverviewHandler) GetSupportedAssets(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	assets, err := h.svc.WalletOverview.GetSupportedAssets(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, assets)
}

// ─── Fiat wallet detail ───────────────────────────────────────────────────────

// GetFiatWalletDetail godoc
//
//	@Summary      Fiat wallet detail
//	@Description  Returns the wallet header (balance, account number, actions) for a specific fiat currency.
//	@Tags         wallets
//	@Produce      json
//	@Security     BearerAuth
//	@Param        currency  path      string  true  "Currency code (USD, EUR, GBP, XAF, XOF)"
//	@Success      200       {object}  map[string]any
//	@Failure      401       {object}  ErrorResponse
//	@Failure      404       {object}  ErrorResponse
//	@Router       /wallets/fiat/{currency} [get]
func (h *WalletOverviewHandler) GetFiatWalletDetail(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	currency := chi.URLParam(r, "currency")
	detail, err := h.svc.WalletOverview.GetFiatWalletDetail(r.Context(), userID, currency)
	if errors.Is(err, services.ErrAccountNotFound) {
		response.NotFound(w, "wallet not found for currency "+strings.ToUpper(currency))
		return
	}
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, detail)
}

// GetFiatTransactions godoc
//
//	@Summary      Fiat wallet transaction history
//	@Description  Returns the transaction history for a fiat wallet with time-period grouping (THIS WEEK / LAST WEEK / EARLIER) and aggregate stats (In / Out / Total). Filterable by type and full-text search.
//	@Tags         wallets
//	@Produce      json
//	@Security     BearerAuth
//	@Param        currency  path    string  true   "Currency code"
//	@Param        type      query   string  false  "Transaction type"  Enums(sent, received, exchanged, deposited, withdrawn)
//	@Param        search    query   string  false  "Search description or reference"
//	@Param        page      query   int     false  "Page (default 1)"
//	@Param        limit     query   int     false  "Results per page (default 20)"
//	@Success      200       {object}  map[string]any
//	@Failure      401       {object}  ErrorResponse
//	@Failure      404       {object}  ErrorResponse
//	@Router       /wallets/fiat/{currency}/transactions [get]
func (h *WalletOverviewHandler) GetFiatTransactions(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	currency := chi.URLParam(r, "currency")
	typeFilter := r.URL.Query().Get("type")
	search := r.URL.Query().Get("search")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	result, err := h.svc.WalletOverview.GetFiatTransactions(
		r.Context(), userID, currency, typeFilter, search, int32(page), int32(limit),
	)
	if errors.Is(err, services.ErrAccountNotFound) {
		response.NotFound(w, "wallet not found for currency "+strings.ToUpper(currency))
		return
	}
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, result)
}

// ─── Crypto wallet detail ─────────────────────────────────────────────────────

// GetCryptoWalletDetail godoc
//
//	@Summary      Crypto wallet detail
//	@Description  Returns the wallet header (live balance, receive address, actions) for a WaaS crypto wallet. Balance is fetched live from the Payments API.
//	@Tags         wallets
//	@Produce      json
//	@Security     BearerAuth
//	@Param        network  path      string  true  "Network (BTC, ETH, SOL, LTC, TRX, POL, BCH, XRP)"
//	@Success      200      {object}  map[string]any
//	@Failure      401      {object}  ErrorResponse
//	@Failure      404      {object}  ErrorResponse
//	@Router       /wallets/crypto/{network} [get]
func (h *WalletOverviewHandler) GetCryptoWalletDetail(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	network := chi.URLParam(r, "network")
	detail, err := h.svc.WalletOverview.GetCryptoWalletDetail(r.Context(), userID, network)
	if errors.Is(err, services.ErrWalletNotFound) {
		response.NotFound(w, "crypto wallet not found — add it via POST /wallets")
		return
	}
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, detail)
}

// GetCryptoTransactions godoc
//
//	@Summary      Crypto wallet transaction history
//	@Description  Returns on-chain transaction history from the Payments API (WaaS) for a specific network.
//	@Tags         wallets
//	@Produce      json
//	@Security     BearerAuth
//	@Param        network  path    string  true   "Network"
//	@Param        page     query   int     false  "Page (default 1)"
//	@Param        limit    query   int     false  "Results per page (default 20)"
//	@Success      200      {object}  map[string]any
//	@Failure      401      {object}  ErrorResponse
//	@Router       /wallets/crypto/{network}/transactions [get]
func (h *WalletOverviewHandler) GetCryptoTransactions(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	network := chi.URLParam(r, "network")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	result, err := h.svc.WalletOverview.GetCryptoTransactions(r.Context(), userID, network, int32(page), int32(limit))
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, result)
}

// ─── Stablecoin wallet detail ─────────────────────────────────────────────────

// GetStablecoinDetail godoc
//
//	@Summary      Stablecoin wallet detail
//	@Description  Returns the wallet header for a stablecoin (USDC). Balance is fetched live from Rach CaaS.
//	@Tags         wallets
//	@Produce      json
//	@Security     BearerAuth
//	@Param        symbol  path      string  true  "Token symbol (USDC)"
//	@Success      200     {object}  map[string]any
//	@Failure      401     {object}  ErrorResponse
//	@Failure      404     {object}  ErrorResponse
//	@Router       /wallets/stablecoin/{symbol} [get]
func (h *WalletOverviewHandler) GetStablecoinDetail(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	symbol := chi.URLParam(r, "symbol")
	detail, err := h.svc.WalletOverview.GetStablecoinDetail(r.Context(), userID, symbol)
	if err != nil {
		response.NotFound(w, "stablecoin wallet not found")
		return
	}
	response.OK(w, detail)
}

// GetStablecoinTransactions godoc
//
//	@Summary      Stablecoin transaction history
//	@Description  Returns CaaS P2P send/receive history for the USDC stablecoin wallet. Maps to the same data as GET /crypto/transactions.
//	@Tags         wallets
//	@Produce      json
//	@Security     BearerAuth
//	@Param        symbol  path    string  true   "Token symbol (USDC)"
//	@Param        page    query   int     false  "Page"
//	@Param        limit   query   int     false  "Limit"
//	@Success      200     {object}  map[string]any
//	@Failure      401     {object}  ErrorResponse
//	@Router       /wallets/stablecoin/{symbol}/transactions [get]
func (h *WalletOverviewHandler) GetStablecoinTransactions(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}

	// Reuse the existing CaaS transaction list.
	txns, err := h.svc.Crypto.ListTransactions(r.Context(), userID, int32(page), int32(limit))
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, map[string]any{
		"transactions": txns,
		"page":         page,
		"limit":        limit,
	})
}
