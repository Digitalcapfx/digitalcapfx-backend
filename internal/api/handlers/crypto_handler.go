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

type CryptoHandler struct {
	svc *services.Services
}

func NewCryptoHandler(svc *services.Services) *CryptoHandler {
	return &CryptoHandler{svc: svc}
}

// GetWallet returns (or creates) the caller's ERC-4337 abstraction wallet.
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

// GetBalances returns USDT and USDC balances from the CaaS service.
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

// Send transfers USDT or USDC to another DigitalFX user by phone number.
func (h *CryptoHandler) Send(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	phone, _ := middleware.UserPhoneFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body struct {
		ReceiverPhone string `json:"receiver_phone"`
		Token         string `json:"token"`  // USDT | USDC
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

func (h *CryptoHandler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "invalid transaction id")
		return
	}
	_ = id
	// TODO: implement GetCryptoTransactionByID in service
	response.OK(w, map[string]string{"message": "not implemented"})
}
