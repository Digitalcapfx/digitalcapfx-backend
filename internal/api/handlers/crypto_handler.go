package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/clients/caas"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type CryptoHandler struct {
	svc *services.Services
}

func NewCryptoHandler(svc *services.Services) *CryptoHandler {
	return &CryptoHandler{svc: svc}
}

// GetWallet godoc
//
//	@Summary      Get or create ERC-4337 abstraction wallet
//	@Description  Returns the caller's ERC-4337 Smart Contract Wallet (SCW), provisioning it on the CaaS service if this is the first call. The wallet is identified by the user's phone number.
//	@Tags         crypto
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  CaasWalletResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      500  {object}  ErrorResponse
//	@Router       /crypto/wallet [get]
func (h *CryptoHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	phone, _ := middleware.UserPhoneFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	wallet, err := h.svc.Crypto.GetOrCreateWallet(r.Context(), userID, phone)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, wallet)
}

// GetBalances godoc
//
//	@Summary      Get stablecoin balances
//	@Description  Returns the USDC balance of the caller's ERC-4337 Smart Contract Wallet from the CaaS service.
//	@Tags         crypto
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  CryptoBalanceResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      500  {object}  ErrorResponse
//	@Router       /crypto/balances [get]
func (h *CryptoHandler) GetBalances(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	balances, err := h.svc.Crypto.GetBalances(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, balances)
}

// FundAccount godoc
//
//	@Summary      Fund Instant USD Account via Mobile Money
//	@Description  Step 4–5 of the Instant USD Account flow. Initiates a HUB2 Mobile Money collection: the customer receives a push-to-pay prompt on their phone. After they approve, HUB2 fires a webhook which triggers DigitalFX to instruct Rach CaaS to convert the XOF/XAF to USDC/USDT and credit the customer's ERC-4337 Smart Contract Wallet. The customer sees their updated balance via GET /crypto/balances once Rach CaaS confirms the fiat and completes the OTC conversion.
//	@Tags         crypto
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      FundAccountRequest  true  "Funding request"
//	@Success      201   {object}  Hub2RefResponse     "HUB2 collection reference — poll status or wait for balance update"
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      500   {object}  ErrorResponse
//	@Router       /crypto/fund [post]
func (h *CryptoHandler) FundAccount(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body FundAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.Currency == "" || body.Amount <= 0 || body.Phone == "" || body.Operator == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "currency, amount, phone and operator are required")
		return
	}
	if body.Token == "" {
		body.Token = "USDC"
	}
	if body.Token != "USDC" && body.Token != "USDT" {
		response.BadRequest(w, "INVALID_TOKEN", "token must be USDC or USDT")
		return
	}

	hub2Ref, err := h.svc.Crypto.InitiateFunding(r.Context(), services.FundingInput{
		UserID:   userID,
		Currency: body.Currency,
		Amount:   body.Amount,
		Phone:    body.Phone,
		Operator: body.Operator,
		Token:    caas.Token(body.Token),
	})
	if err != nil {
		if err == services.ErrAccountNotFound {
			response.BadRequest(w, "USER_NOT_FOUND", "user account not found")
			return
		}
		response.InternalError(w)
		return
	}

	response.Created(w, Hub2RefData{Hub2Reference: hub2Ref})
}

// Send godoc
//
//	@Summary      Send USDT or USDC to another user
//	@Description  Transfers stablecoins (USDT or USDC) from the caller to another DigitalFX user identified by phone number. An FX quote is obtained first, then the CaaS P2P transfer is executed. Amount is in USD-equivalent decimal (e.g. "50.00").
//	@Tags         crypto
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      SendCryptoRequest  true  "Transfer details"
//	@Success      201   {object}  CryptoTxResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      500   {object}  ErrorResponse
//	@Router       /crypto/send [post]
func (h *CryptoHandler) Send(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	phone, _ := middleware.UserPhoneFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body struct {
		ReceiverPhone string `json:"receiver_phone"`
		Token         string `json:"token"`
		Amount        string `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.ReceiverPhone == "" || body.Token == "" || body.Amount == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "receiver_phone, token and amount are required")
		return
	}
	if body.Token != "USDT" && body.Token != "USDC" {
		response.BadRequest(w, "INVALID_TOKEN", "token must be USDT or USDC")
		return
	}

	tx, err := h.svc.Crypto.Send(r.Context(), services.SendCryptoInput{
		SenderUserID:  userID,
		SenderPhone:   phone,
		ReceiverPhone: body.ReceiverPhone,
		Token:         body.Token,
		Amount:        body.Amount,
	})
	if err != nil {
		response.InternalError(w)
		return
	}

	response.Created(w, tx)
}

// ListTransactions godoc
//
//	@Summary      List crypto transactions
//	@Description  Returns a paginated list of crypto (USDT/USDC) transactions for the authenticated user.
//	@Tags         crypto
//	@Produce      json
//	@Security     BearerAuth
//	@Param        page      query     int  false  "Page number (default 1)"
//	@Param        per_page  query     int  false  "Results per page, max 100 (default 20)"
//	@Success      200       {object}  CryptoTxListResponse
//	@Failure      401       {object}  ErrorResponse
//	@Failure      500       {object}  ErrorResponse
//	@Router       /crypto/transactions [get]
func (h *CryptoHandler) ListTransactions(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	txs, err := h.svc.Crypto.ListTransactions(r.Context(), userID, int32(page), int32(perPage))
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, txs)
}

// GetTransaction godoc
//
//	@Summary      Get a crypto transaction
//	@Description  Returns a single crypto transaction by its UUID.
//	@Tags         crypto
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id  path      string  true  "Transaction UUID"
//	@Success      200 {object}  CryptoTxResponse
//	@Failure      400 {object}  ErrorResponse  "Invalid UUID"
//	@Failure      401 {object}  ErrorResponse
//	@Failure      404 {object}  ErrorResponse
//	@Router       /crypto/transactions/{id} [get]
func (h *CryptoHandler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "invalid transaction id")
		return
	}
	_ = id
	response.OK(w, map[string]string{"message": "not implemented"})
}
