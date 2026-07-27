package handlers

import (
	"errors"
	"io"
	"net/http"

	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

// NombaWebhookHandler receives webhooks from Nomba (Nigerian NGN rails). Inbound
// virtual-account credits (payment_success) are applied to the owning account.
type NombaWebhookHandler struct {
	svc    *services.Services
	logger *zap.Logger
}

func NewNombaWebhookHandler(svc *services.Services, logger *zap.Logger) *NombaWebhookHandler {
	return &NombaWebhookHandler{svc: svc, logger: logger}
}

// Receive godoc
//
//	@Summary      Nomba Webhook (NGN virtual accounts + payouts)
//	@Description  Receiver for Nomba account/transfer events. On `payment_success` (a virtual-account credit) the owning DigitalFX Naira account is credited by the alias account number and the customer is notified. Signature is verified against NOMBA_WEBHOOK_SECRET using the `nomba-signature` header (HmacSHA256 of the documented field string). Payout events are logged. Returns 200 on business-logic issues so Nomba does not retry; 401 only on signature mismatch.
//	@Tags         webhooks
//	@Accept       json
//	@Produce      json
//	@Param        nomba-signature  header    string              false  "Base64 HMAC-SHA256 signature"
//	@Param        body             body      nomba.WebhookEvent  true   "Nomba webhook payload (event_type: payment_success | payment_failed | payment_reversal | payout_success | payout_failed | payout_refund)"
//	@Success      200              {object}  MessageResponse
//	@Failure      401              {object}  ErrorResponse
//	@Router       /webhooks/nomba [post]
func (h *NombaWebhookHandler) Receive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB cap
	if err != nil {
		response.BadRequest(w, "READ_ERROR", "failed to read request body")
		return
	}

	if err := h.svc.Nomba.HandleWebhook(r.Context(), body, r.Header); err != nil {
		if errors.Is(err, services.ErrNombaWebhookSignature) {
			response.Unauthorized(w, "invalid webhook signature")
			return
		}
		// Any other error is a processing issue — log and still 200 so Nomba does
		// not hammer us with retries for a non-retryable condition.
		h.logger.Error("nomba webhook processing error", zap.Error(err))
	}

	response.OKWithMessage(w, "received", nil)
}
