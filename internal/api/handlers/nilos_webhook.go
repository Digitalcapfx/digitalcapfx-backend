package handlers

import (
	"errors"
	"io"
	"net/http"

	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

// NilosWebhookHandler receives webhooks from Nilos (EUR/GBP/… fiat rails).
// Inbound, successful deposits credit the owning account's ledger (idempotently).
type NilosWebhookHandler struct {
	svc    *services.Services
	logger *zap.Logger
}

func NewNilosWebhookHandler(svc *services.Services, logger *zap.Logger) *NilosWebhookHandler {
	return &NilosWebhookHandler{svc: svc, logger: logger}
}

// Receive godoc
//
//	@Summary      Nilos Webhook (fiat deposits)
//	@Description  Receiver for Nilos transaction events. An inbound, successful transaction credits the owning DigitalFX fiat account (resolved by the Nilos account id) — idempotently, keyed on the Nilos transaction id, so redelivery never double-credits. Signature is verified against NILOS_WEBHOOK_SECRET when set (the Nilos webhook scheme is confirmed on first delivery — mismatches log the headers). Returns 200 on business-logic issues so Nilos does not retry; 401 only on signature mismatch.
//	@Tags         webhooks
//	@Accept       json
//	@Produce      json
//	@Param        body  body      map[string]any  true  "Nilos webhook payload"
//	@Success      200   {object}  MessageResponse
//	@Failure      401   {object}  ErrorResponse
//	@Router       /webhooks/nilos [post]
func (h *NilosWebhookHandler) Receive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB cap
	if err != nil {
		response.BadRequest(w, "READ_ERROR", "failed to read request body")
		return
	}

	if err := h.svc.Nilos.HandleWebhook(r.Context(), body, r.Header); err != nil {
		if errors.Is(err, services.ErrNilosWebhookSignature) {
			response.Unauthorized(w, "invalid webhook signature")
			return
		}
		h.logger.Error("nilos webhook processing error", zap.Error(err))
	}

	response.OKWithMessage(w, "received", nil)
}
