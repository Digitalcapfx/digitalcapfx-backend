package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type WalletHandler struct {
	svc *services.Services
}

func NewWalletHandler(svc *services.Services) *WalletHandler {
	return &WalletHandler{svc: svc}
}

// ListWallets godoc
//
//	@Summary      List HD wallets
//	@Description  Returns all on-chain HD wallets (BIP-44) provisioned for the authenticated user via the Rach WaaS service.
//	@Tags         WaaS - Crypto Wallets
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  WalletListResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      500  {object}  ErrorResponse
//	@Router       /wallets [get]
func (h *WalletHandler) ListWallets(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	wallets, err := h.svc.Wallet.ListWallets(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, wallets)
}

// CreateWallet godoc
//
//	@Summary      Create HD wallet for a network
//	@Description  Provisions an HD wallet seed (if not already created) and derives a deposit address for the given blockchain network. Idempotent — safe to call again for the same network.
//	@Tags         WaaS - Crypto Wallets
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      CreateWalletRequest  true  "Network selection"
//	@Success      201   {object}  WalletResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      500   {object}  ErrorResponse
//	@Router       /wallets [post]
func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body struct {
		Network string `json:"network"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Network == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "network is required")
		return
	}

	wallet, err := h.svc.Wallet.CreateWallet(r.Context(), userID, body.Network)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.Created(w, wallet)
}

// GetDepositAddress godoc
//
//	@Summary      Get deposit address
//	@Description  Returns the deposit address details for the specified wallet ID.
//	@Tags         WaaS - Crypto Wallets
//	@Produce      json
//	@Security     BearerAuth
//	@Param        walletId  path      string  true  "Wallet UUID"
//	@Success      200       {object}  db.WaasWallet
//	@Failure      400       {object}  ErrorResponse
//	@Failure      401       {object}  ErrorResponse
//	@Failure      404       {object}  ErrorResponse
//	@Router       /wallets/{walletId}/address [get]
func (h *WalletHandler) GetDepositAddress(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	walletUUID, err := uuid.Parse(chi.URLParam(r, "walletId"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "invalid wallet ID format")
		return
	}

	wallet, err := h.svc.Wallet.GetWaasWallet(r.Context(), walletUUID, userID)
	if err != nil {
		response.NotFound(w, "wallet not found")
		return
	}

	response.OK(w, wallet)
}

// InitiateDeposit godoc
//
//	@Summary      Initiate Mobile Money deposit
//	@Description  Initiates a Mobile Money collection (deposit) via HUB2, crediting the user's fiat account when confirmed. Returns the HUB2 reference for webhook correlation.
//	@Tags         Mobile Money - HUB2
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      DepositRequest   true  "Deposit details"
//	@Success      201   {object}  Hub2RefResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      500   {object}  ErrorResponse
//	@Router       /wallets/deposit [post]
func (h *WalletHandler) InitiateDeposit(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body struct {
		Currency string  `json:"currency"`
		Amount   float64 `json:"amount"`
		Phone    string  `json:"phone"`
		Operator string  `json:"operator"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}

	ref, err := h.svc.Wallet.InitiateDeposit(r.Context(), services.DepositInput{
		UserID:   userID,
		Currency: body.Currency,
		Amount:   body.Amount,
		Phone:    body.Phone,
		Operator: body.Operator,
	})
	if err != nil {
		response.InternalError(w)
		return
	}

	response.Created(w, map[string]string{"hub2_reference": ref})
}

// InitiateWithdrawal godoc
//
//	@Summary      Initiate Mobile Money withdrawal
//	@Description  Initiates a Mobile Money disbursement (withdrawal) via HUB2, debiting the user's fiat balance.
//	@Tags         Mobile Money - HUB2
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      WithdrawalRequest  true  "Withdrawal details"
//	@Success      201   {object}  Hub2RefResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      500   {object}  ErrorResponse
//	@Router       /wallets/withdraw [post]
func (h *WalletHandler) InitiateWithdrawal(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body struct {
		Currency string  `json:"currency"`
		Amount   float64 `json:"amount"`
		Phone    string  `json:"phone"`
		Operator string  `json:"operator"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}

	ref, err := h.svc.Wallet.InitiateWithdrawal(r.Context(), services.WithdrawInput{
		UserID:   userID,
		Currency: body.Currency,
		Amount:   body.Amount,
		Phone:    body.Phone,
		Operator: body.Operator,
	})
	if err != nil {
		response.InternalError(w)
		return
	}

	response.Created(w, map[string]string{"hub2_reference": ref})
}

// ─── WaaS full surface (send, balances, transactions, gas, seed, key) ──────────

// TransferCryptoRequest is the body for POST /wallets/transfer.
type TransferCryptoRequest struct {
	Network   string `json:"network" example:"POL"`
	Currency  string `json:"currency" example:"USDT"`
	ToAddress string `json:"to_address" example:"0x1234abcd..."`
	Amount    string `json:"amount" example:"1000000"` // smallest on-chain unit (wei/sat/lamport/sun/drop)
	Index     uint32 `json:"index,omitempty"`
}

// TransferCrypto godoc
//
//	@Summary      Send crypto on-chain (WaaS)
//	@Description  Broadcasts an on-chain transfer of a native coin or on-chain token from the caller's Rach WaaS HD wallet to an external address. Amount is in the SMALLEST on-chain unit (wei / satoshi / lamport / sun / drop). Currency is the coin/token symbol (POL, ETH, BNB, USDT, USDC, …). This is the WaaS rail — distinct from CaaS USDC Phone Send.
//	@Tags         WaaS - Crypto Wallets
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      TransferCryptoRequest  true  "Transfer details"
//	@Success      201   {object}  payments.TransferResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      500   {object}  ErrorResponse
//	@Router       /wallets/transfer [post]
func (h *WalletHandler) TransferCrypto(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	var body TransferCryptoRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.Network == "" || body.Currency == "" || body.ToAddress == "" || body.Amount == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "network, currency, to_address and amount are required")
		return
	}
	res, err := h.svc.Wallet.TransferCrypto(r.Context(), userID, body.Network, body.Currency, body.ToAddress, body.Amount, body.Index)
	if err != nil {
		response.InternalError(w)
		return
	}
	response.Created(w, res)
}

// ListAddresses godoc
//
//	@Summary      List WaaS addresses & balances
//	@Description  Returns every derived on-chain address for the caller across all networks, each with its live per-currency balances (native coin + on-chain stablecoins). Pass ?refresh=true to force a live on-chain balance refresh (slower, always accurate).
//	@Tags         WaaS - Crypto Wallets
//	@Produce      json
//	@Security     BearerAuth
//	@Param        refresh  query  bool  false  "Force a live on-chain balance refresh"
//	@Success      200  {object}  payments.ListCustomerAddressesResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      500  {object}  ErrorResponse
//	@Router       /wallets/addresses [get]
func (h *WalletHandler) ListAddresses(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	res, err := h.svc.Wallet.ListCustomerAddresses(r.Context(), userID, r.URL.Query().Get("refresh") == "true")
	if err != nil {
		// No WaaS wallet provisioned yet (or upstream hiccup) → empty list, not 500.
		response.OK(w, map[string]any{"customer_id": userID.String(), "addresses": []any{}, "total": 0})
		return
	}
	response.OK(w, res)
}

// GetWaasTransactions godoc
//
//	@Summary      WaaS on-chain transactions
//	@Description  Returns the caller's on-chain transaction history from Rach WaaS, filterable by network, currency and status. This is real blockchain history, distinct from the CaaS USDC ledger.
//	@Tags         WaaS - Crypto Wallets
//	@Produce      json
//	@Security     BearerAuth
//	@Param        page      query  int     false  "Page (default 1)"
//	@Param        limit     query  int     false  "Results per page (default 20, max 100)"
//	@Param        network   query  string  false  "Filter by network (BTC, ETH, BSC, POL, TRX, SOL, LTC, BCH, XRP)"
//	@Param        currency  query  string  false  "Filter by currency (POL, ETH, USDT, USDC, …)"
//	@Param        status    query  string  false  "Filter by status (pending, confirmed, failed)"
//	@Success      200  {object}  payments.GetTransactionsResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      500  {object}  ErrorResponse
//	@Router       /wallets/transactions [get]
func (h *WalletHandler) GetWaasTransactions(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	res, err := h.svc.Wallet.GetWaasTransactions(r.Context(), userID, page, limit,
		r.URL.Query().Get("network"), r.URL.Query().Get("currency"), r.URL.Query().Get("status"))
	if err != nil {
		// No WaaS wallet / no history yet → empty page, not 500.
		response.OK(w, map[string]any{"customer_id": userID.String(), "transactions": []any{}, "total": 0, "page": page, "limit": limit})
		return
	}
	response.OK(w, res)
}

// EstimateGasRequestBody is the body for POST /wallets/estimate-gas.
type EstimateGasRequestBody struct {
	Network     string `json:"network" example:"POL"`
	Currency    string `json:"currency" example:"USDT"`
	FromAddress string `json:"from_address" example:"0xabc..."`
	ToAddress   string `json:"to_address" example:"0xdef..."`
	Amount      string `json:"amount" example:"1000000"`
}

// EstimateGas godoc
//
//	@Summary      Estimate on-chain gas (WaaS)
//	@Description  Estimates the network fee for an EVM transfer (BSC, ETH, POL). Amount is in wei.
//	@Tags         WaaS - Crypto Wallets
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      EstimateGasRequestBody  true  "Gas estimate request"
//	@Success      200   {object}  payments.GasEstimate
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      500   {object}  ErrorResponse
//	@Router       /wallets/estimate-gas [post]
func (h *WalletHandler) EstimateGas(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.UserIDFromContext(r.Context()); !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	var body EstimateGasRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.Network == "" || body.Currency == "" || body.FromAddress == "" || body.ToAddress == "" || body.Amount == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "network, currency, from_address, to_address and amount are required")
		return
	}
	res, err := h.svc.Wallet.EstimateGas(r.Context(), body.Network, body.Currency, body.FromAddress, body.ToAddress, body.Amount)
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, res)
}

// RevealSeed godoc
//
//	@Summary      Reveal WaaS seed phrase (SENSITIVE)
//	@Description  Returns the BIP-39 mnemonic for the caller's Rach WaaS HD wallet. HIGHLY SENSITIVE — anyone with this phrase controls ALL of the user's crypto. Intended for self-custody export; gate behind step-up auth (PIN/biometric/2FA) before surfacing in any UI.
//	@Tags         WaaS - Crypto Wallets
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  payments.GetSeedPhraseResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      500  {object}  ErrorResponse
//	@Router       /wallets/seed [get]
func (h *WalletHandler) RevealSeed(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	res, err := h.svc.Wallet.GetSeedPhrase(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, res)
}

// ExportKeyRequest is the body for POST /wallets/export-key.
type ExportKeyRequest struct {
	Network string `json:"network" example:"POL"`
	Index   uint32 `json:"index,omitempty"`
}

// ExportPrivateKey godoc
//
//	@Summary      Export WaaS private key (SENSITIVE)
//	@Description  Exports the private key for one of the caller's derived WaaS addresses. HIGHLY SENSITIVE — grants full control of that address. Index must match the derivation index used when the address was created (default 0). Gate behind step-up auth before surfacing in any UI.
//	@Tags         WaaS - Crypto Wallets
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      ExportKeyRequest  true  "Network + derivation index"
//	@Success      200   {object}  payments.ExportPrivateKeyResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      500   {object}  ErrorResponse
//	@Router       /wallets/export-key [post]
func (h *WalletHandler) ExportPrivateKey(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	var body ExportKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Network == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "network is required")
		return
	}
	res, err := h.svc.Wallet.ExportPrivateKey(r.Context(), userID, body.Network, body.Index)
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, res)
}
