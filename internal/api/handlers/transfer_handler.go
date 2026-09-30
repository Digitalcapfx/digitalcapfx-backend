package handlers

import (
	"encoding/json"
	"errors"
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

// InternalTransfer godoc
//
//	@Summary      Internal fiat transfer
//	@Description  Transfers fiat funds between two DigitalFX users using their phone numbers. Both sender and receiver must have an account in the requested currency.
//	@Tags         Fiat - Nilos
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      InternalTransferRequest  true  "Transfer details"
//	@Success      200   {object}  MessageResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      500   {object}  ErrorResponse
//	@Router       /transfers/internal [post]
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
	if body.ReceiverPhone == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "receiver_phone is required")
		return
	}

	res, err := h.svc.Transfer.InternalTransfer(r.Context(), services.InternalTransferInput{
		SenderID:      userID,
		ReceiverPhone: body.ReceiverPhone,
		Currency:      body.Currency,
		Amount:        body.Amount,
		Description:   body.Description,
	})
	if err != nil {
		switch {
		case errors.Is(err, services.ErrTransferInvalidAmount):
			response.BadRequest(w, "INVALID_AMOUNT", err.Error())
		case errors.Is(err, services.ErrUnsupportedCurrency):
			response.BadRequest(w, "UNSUPPORTED_CURRENCY", "instant transfers are only available for XAF and XOF")
		case errors.Is(err, services.ErrTransferReceiverNotFound):
			response.BadRequest(w, "RECEIVER_NOT_FOUND", err.Error())
		case errors.Is(err, services.ErrTransferToSelf):
			response.BadRequest(w, "INVALID_RECEIVER", err.Error())
		case errors.Is(err, services.ErrAccountNotFound):
			response.BadRequest(w, "ACCOUNT_NOT_FOUND", "sender or receiver has no account in that currency")
		case errors.Is(err, services.ErrInsufficientFunds):
			response.BadRequest(w, "INSUFFICIENT_FUNDS", "insufficient balance")
		default:
			response.InternalError(w)
		}
		return
	}
	response.OK(w, res)
}

// Hub2Payment godoc
//
//	@Summary      Mobile Money payment via HUB2
//	@Description  Initiates either a collection (deposit) or disbursement (withdrawal) via the HUB2 Mobile Money gateway for XAF/XOF operators (MTN, Orange).
//	@Tags         Mobile Money - HUB2
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      Hub2PaymentRequest  true  "Payment details"
//	@Success      201   {object}  Hub2RefResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      500   {object}  ErrorResponse
//	@Router       /transfers/hub2 [post]
func (h *TransferHandler) Hub2Payment(w http.ResponseWriter, r *http.Request) {
	// Deprecated: use POST /api/v1/crypto/fund to fund the Stablecoin Account
	// via Mobile Money. This generic endpoint is no longer active.
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	response.BadRequest(w, "DEPRECATED", "use POST /api/v1/crypto/fund to fund your Stablecoin Account")
}

// ExchangeCurrency godoc
//
//	@Summary      Exchange currency
//	@Description  Converts between supported fiat currencies at the current FX rate. Rate provider integration is pending.
//	@Tags         Fiat - Nilos
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  MessageResponse
//	@Failure      401  {object}  ErrorResponse
//	@Router       /transfers/exchange [post]
func (h *TransferHandler) ExchangeCurrency(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	response.OK(w, map[string]string{"message": "not implemented"})
}
