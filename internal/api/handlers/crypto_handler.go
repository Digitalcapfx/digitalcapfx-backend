package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/clients/caas"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type CaaSHandler struct {
	svc *services.Services
}

func NewCaaSHandler(svc *services.Services) *CaaSHandler {
	return &CaaSHandler{svc: svc}
}

// GetWallet godoc
//
//	@Summary      Get or create the user's USDC (CaaS) wallet
//	@Description  Returns the caller's Stablecoin (USDC) wallet — an EIP-4337 Smart Contract Wallet (SCW) provisioned on Rach CaaS, keyed by the user's phone number (provisioned on first call). This is the CaaS rail (instant dollars that settle on-chain as USDC), NOT the Rach WaaS HD crypto wallets. Customer-facing unit is always USDC.
//	@Tags         CaaS - Stablecoin (USDC)
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  CaasWalletResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      500  {object}  ErrorResponse
//	@Router       /crypto/wallet [get]
func (h *CaaSHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	phone, _ := middleware.UserPhoneFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	wallet, err := h.svc.CaaS.GetOrCreateWallet(r.Context(), userID, phone)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, wallet)
}

// GetBalances godoc
//
//	@Summary      Get USDC balance
//	@Description  Returns the caller's Stablecoin (USDC) balance from Rach CaaS. It settles on-chain as USDC inside the EIP-4337 Smart Contract Wallet. This is separate from any USDT/USDC held in the Rach WaaS crypto wallets.
//	@Tags         CaaS - Stablecoin (USDC)
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  CryptoBalanceResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      500  {object}  ErrorResponse
//	@Router       /crypto/balances [get]
func (h *CaaSHandler) GetBalances(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	balances, err := h.svc.CaaS.GetBalances(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, balances)
}

// FundAccount godoc
//
//	@Summary      Fund USDC account via Mobile Money
//	@Description  Funds the customer's Stablecoin (USDC) account. Initiates a HUB2 Mobile Money collection: the customer receives a push-to-pay prompt on their phone. After they approve, HUB2 fires a webhook which triggers Rach CaaS to convert the XOF/XAF to on-chain USDC and credit the customer's EIP-4337 Smart Contract Wallet — reflected to the customer as USDC. Poll GET /crypto/balances for the updated USDC balance once CaaS confirms the fiat and completes the conversion.
//	@Tags         CaaS - Stablecoin (USDC)
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      FundAccountRequest  true  "Funding request"
//	@Success      201   {object}  Hub2RefResponse     "HUB2 collection reference — poll status or wait for balance update"
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      500   {object}  ErrorResponse
//	@Router       /crypto/fund [post]
func (h *CaaSHandler) FundAccount(w http.ResponseWriter, r *http.Request) {
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

	hub2Ref, err := h.svc.CaaS.InitiateFunding(r.Context(), services.FundingInput{
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
//	@Summary      Send USDC to another user (Phone Send)
//	@Description  Transfers Stablecoin (USDC) from the caller to another DigitalFX user identified by phone number, via Rach CaaS (settles on-chain as USDC between the two EIP-4337 wallets). Amount is USD-equivalent decimal (e.g. "50.00"). Note: the `token` field selects the on-chain settlement asset (USDC/USDT) but the customer-facing unit is USDC. This is the CaaS P2P rail, distinct from WaaS on-chain crypto transfers.
//	@Tags         CaaS - Stablecoin (USDC)
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      SendCryptoRequest  true  "Transfer details"
//	@Success      201   {object}  CryptoTxResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      500   {object}  ErrorResponse
//	@Router       /crypto/send [post]
func (h *CaaSHandler) Send(w http.ResponseWriter, r *http.Request) {
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

	tx, err := h.svc.CaaS.Send(r.Context(), services.SendCryptoInput{
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

	h.svc.Notifications.Create(r.Context(), services.CreateNotificationInput{
		UserID: userID,
		Type:   services.NotifCryptoSent,
		Title:  "Crypto Sent",
		Body:   fmt.Sprintf("You sent %s %s to %s.", body.Amount, body.Token, body.ReceiverPhone),
		Metadata: map[string]string{
			"token":          body.Token,
			"amount":         body.Amount,
			"receiver_phone": body.ReceiverPhone,
		},
	})

	response.Created(w, tx)
}

// ListTransactions godoc
//
//	@Summary      List USDC (CaaS) transactions
//	@Description  Returns a paginated list of the user's Stablecoin (USDC / CaaS) transactions — funding, Phone Send transfers, and off-ramp withdrawals. These are CaaS-rail movements, not WaaS on-chain crypto history (see /wallets/crypto/{network}/transactions for that).
//	@Tags         CaaS - Stablecoin (USDC)
//	@Produce      json
//	@Security     BearerAuth
//	@Param        page      query     int  false  "Page number (default 1)"
//	@Param        per_page  query     int  false  "Results per page, max 100 (default 20)"
//	@Success      200       {object}  CryptoTxListResponse
//	@Failure      401       {object}  ErrorResponse
//	@Failure      500       {object}  ErrorResponse
//	@Router       /crypto/transactions [get]
func (h *CaaSHandler) ListTransactions(w http.ResponseWriter, r *http.Request) {
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

	txs, err := h.svc.CaaS.ListTransactions(r.Context(), userID, int32(page), int32(perPage))
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, txs)
}

// GetTransaction godoc
//
//	@Summary      Get an USDC (CaaS) transaction
//	@Description  Returns a single Stablecoin (USDC / CaaS) transaction by its UUID.
//	@Tags         CaaS - Stablecoin (USDC)
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id  path      string  true  "Transaction UUID"
//	@Success      200 {object}  CryptoTxResponse
//	@Failure      400 {object}  ErrorResponse  "Invalid UUID"
//	@Failure      401 {object}  ErrorResponse
//	@Failure      404 {object}  ErrorResponse
//	@Router       /crypto/transactions/{id} [get]
func (h *CaaSHandler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "invalid transaction id")
		return
	}
	_ = id
	response.OK(w, map[string]string{"message": "not implemented"})
}

// Withdraw godoc
//
//	@Summary      Withdraw USDC to Mobile Money (off-ramp)
//	@Description  Off-ramps Stablecoin (USDC) from the user's EIP-4337 Smart Contract Wallet back to fiat on their Mobile Money number, via Rach CaaS (settles the on-chain USDC and pays out local currency). The `token` field is the on-chain settlement asset; the customer balance is USDC.
//	@Tags         CaaS - Stablecoin (USDC)
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      WithdrawCryptoRequest  true  "Withdrawal details"
//	@Success      201   {object}  db.CaasWithdrawal
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      500   {object}  ErrorResponse
//	@Router       /crypto/withdraw [post]
func (h *CaaSHandler) Withdraw(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	phone, _ := middleware.UserPhoneFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body WithdrawCryptoRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.Amount == "" || body.Token == "" || body.PayoutMobile == "" || body.PayoutNetwork == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "amount, token, payout_mobile, and payout_network are required")
		return
	}
	if body.Token != "USDT" && body.Token != "USDC" {
		response.BadRequest(w, "INVALID_TOKEN", "token must be USDT or USDC")
		return
	}

	idempotencyKey := fmt.Sprintf("WTH-%s", uuid.New().String())

	withdrawal, err := h.svc.CaaS.Withdraw(r.Context(), services.WithdrawCryptoInput{
		UserID:         userID,
		Phone:          phone,
		Amount:         body.Amount,
		Token:          caas.Token(body.Token),
		PayoutMobile:   body.PayoutMobile,
		PayoutNetwork:  body.PayoutNetwork,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		response.InternalError(w)
		return
	}

	response.Created(w, withdrawal)
}
