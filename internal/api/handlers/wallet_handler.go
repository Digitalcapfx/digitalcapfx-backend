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
//	@Tags         wallets
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
//	@Tags         wallets
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
//	@Tags         wallets
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
//	@Tags         wallets
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

// GetSwapQuote godoc
//
//	@Summary      Get a WaaS token swap quote
//	@Description  Returns a rate quote for swapping tokens across chains using WaaS swap.
//	@Tags         wallets
//	@Produce      json
//	@Security     BearerAuth
//	@Param        from_chain  query     string  true  "Source chain"
//	@Param        to_chain    query     string  true  "Destination chain"
//	@Param        from_token  query     string  true  "Source token"
//	@Param        to_token    query     string  true  "Destination token"
//	@Param        amount_in   query     string  true  "Amount in base units"
//	@Success      200         {object}  payments.SwapQuoteResponse
//	@Failure      400         {object}  ErrorResponse
//	@Failure      401         {object}  ErrorResponse
//	@Failure      500         {object}  ErrorResponse
//	@Router       /wallets/swap/quote [get]
func (h *WalletHandler) GetSwapQuote(w http.ResponseWriter, r *http.Request) {
	fromChain := r.URL.Query().Get("from_chain")
	toChain := r.URL.Query().Get("to_chain")
	fromToken := r.URL.Query().Get("from_token")
	toToken := r.URL.Query().Get("to_token")
	amountIn := r.URL.Query().Get("amount_in")

	if fromChain == "" || toChain == "" || fromToken == "" || toToken == "" || amountIn == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "from_chain, to_chain, from_token, to_token, and amount_in are required")
		return
	}

	quote, err := h.svc.Wallet.GetSwapQuote(r.Context(), fromChain, toChain, fromToken, toToken, amountIn)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, quote)
}

// ExecuteSwapRequest is the incoming payload for ExecuteSwap
type ExecuteSwapRequest struct {
	FromChain    string `json:"from_chain"`
	ToChain      string `json:"to_chain"`
	FromToken    string `json:"from_token"`
	ToToken      string `json:"to_token"`
	AmountIn     string `json:"amount_in"`
	AmountOutMin string `json:"amount_out_min"`
}

// ExecuteSwap godoc
//
//	@Summary      Execute a WaaS token swap
//	@Description  Broadcasts a swap transaction from the caller's WaaS wallet using a previously quoted route.
//	@Tags         wallets
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      ExecuteSwapRequest  true  "Swap details"
//	@Success      201   {object}  payments.ExecuteSwapResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      500   {object}  ErrorResponse
//	@Router       /wallets/swap/execute [post]
func (h *WalletHandler) ExecuteSwap(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body ExecuteSwapRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.FromChain == "" || body.ToChain == "" || body.FromToken == "" || body.ToToken == "" || body.AmountIn == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "from_chain, to_chain, from_token, to_token, and amount_in are required")
		return
	}

	result, err := h.svc.Wallet.ExecuteSwap(r.Context(), userID, body.FromChain, body.ToChain, body.FromToken, body.ToToken, body.AmountIn, body.AmountOutMin)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.Created(w, result)
}

// GetSwapHistory godoc
//
//	@Summary      Get WaaS swap history
//	@Description  Returns the caller's paginated swap transaction history.
//	@Tags         wallets
//	@Produce      json
//	@Security     BearerAuth
//	@Param        page   query     int  false  "Page number (default 1)"
//	@Param        limit  query     int  false  "Results per page (default 20)"
//	@Success      200    {object}  payments.GetSwapHistoryResponse
//	@Failure      401    {object}  ErrorResponse
//	@Failure      500    {object}  ErrorResponse
//	@Router       /wallets/swap/history [get]
func (h *WalletHandler) GetSwapHistory(w http.ResponseWriter, r *http.Request) {
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

	history, err := h.svc.Wallet.GetSwapHistory(r.Context(), userID, page, limit)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, history)
}

// InitiateWithdrawal godoc
//
//	@Summary      Initiate Mobile Money withdrawal
//	@Description  Initiates a Mobile Money disbursement (withdrawal) via HUB2, debiting the user's fiat balance.
//	@Tags         wallets
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
