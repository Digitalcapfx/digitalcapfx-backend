package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

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
//	@Description  Returns the deposit address for the specified wallet ID.
//	@Tags         wallets
//	@Produce      json
//	@Security     BearerAuth
//	@Param        walletId  path      string  true  "Wallet UUID"
//	@Success      200       {object}  WalletResponse
//	@Failure      401       {object}  ErrorResponse
//	@Failure      404       {object}  ErrorResponse
//	@Router       /wallets/{walletId}/address [get]
func (h *WalletHandler) GetDepositAddress(w http.ResponseWriter, r *http.Request) {
	walletID := chi.URLParam(r, "walletId")
	_ = walletID
	response.OK(w, map[string]string{"message": "not implemented"})
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

// InitiateWithdrawal godoc
//
//	@Summary      Initiate Mobile Money withdrawal
//	@Description  Initiates a Mobile Money disbursement (withdrawal) via HUB2, debiting the user's fiat account. Returns the HUB2 reference for tracking.
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
