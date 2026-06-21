package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type TransferHandler struct {
	svc *services.Services
}

func NewTransferHandler(svc *services.Services) *TransferHandler {
	return &TransferHandler{svc: svc}
}

// InternalTransfer moves fiat between two DigitalFX accounts.
func (h *TransferHandler) InternalTransfer(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body struct {
		ReceiverPhone string  `json:"receiver_phone"`
		Currency      string  `json:"currency"`
		Amount        float64 `json:"amount"`
		Description   string  `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}

	_ = userID
	// TODO: implement in AccountService / TransferService
	response.OK(w, map[string]string{"message": "not implemented"})
}

// Hub2Payment initiates a Mobile Money deposit or withdrawal via HUB2.
func (h *TransferHandler) Hub2Payment(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body struct {
		Currency      string  `json:"currency"`
		Amount        float64 `json:"amount"`
		Phone         string  `json:"phone"`
		Operator      string  `json:"operator"`
		PaymentMethod string  `json:"payment_method"`
		Direction     string  `json:"direction"` // collection | disbursement
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}

	payment, err := h.svc.HUB2.InitiatePayment(r.Context(), services.Hub2PaymentInput{
		UserID:        userID,
		Currency:      body.Currency,
		Amount:        body.Amount,
		Phone:         body.Phone,
		Operator:      body.Operator,
		PaymentMethod: body.PaymentMethod,
		Direction:     body.Direction,
	})
	if err != nil {
		response.InternalError(w)
		return
	}

	response.Created(w, payment)
}

// ExchangeCurrency converts between supported fiat currencies (FX rates TBD).
func (h *TransferHandler) ExchangeCurrency(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	// TODO: integrate FX rate provider and implement exchange
	response.OK(w, map[string]string{"message": "not implemented"})
}
