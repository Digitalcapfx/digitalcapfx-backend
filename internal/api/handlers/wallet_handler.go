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

func (h *WalletHandler) GetDepositAddress(w http.ResponseWriter, r *http.Request) {
	walletID := chi.URLParam(r, "walletId")
	_ = walletID
	// TODO: fetch from Payments API
	response.OK(w, map[string]string{"message": "not implemented"})
}

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
