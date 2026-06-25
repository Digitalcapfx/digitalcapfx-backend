package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"go.uber.org/zap"

	hub2client "github.com/rachfinance/digitalfx/internal/clients/hub2"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type WebhookHandler struct {
	svc        *services.Services
	hub2Secret string
	logger     *zap.Logger
}

func NewWebhookHandler(svc *services.Services, hub2Secret string, logger *zap.Logger) *WebhookHandler {
	return &WebhookHandler{svc: svc, hub2Secret: hub2Secret, logger: logger}
}

// HUB2Webhook godoc
//
//	@Summary      HUB2 payment webhook
//	@Description  Receives Mobile Money payment status updates from HUB2. On a COLLECTION SUCCESSFUL event, DigitalFX automatically credits the user's CaaS USDC wallet. The request must carry a valid HMAC-SHA256 signature in the X-Hub2-Signature header.
//	@Tags         webhooks
//	@Accept       json
//	@Produce      json
//	@Param        X-Hub2-Signature  header    string          true  "HMAC-SHA256 signature: sha256=<hex>"
//	@Param        body              body      HUB2WebhookRequest  true  "HUB2 webhook payload"
//	@Success      200               {object}  MessageResponse
//	@Failure      400               {object}  ErrorResponse
//	@Failure      403               {object}  ErrorResponse  "Invalid signature"
//	@Failure      500               {object}  ErrorResponse
//	@Router       /webhooks/hub2 [post]
func (h *WebhookHandler) HUB2(w http.ResponseWriter, r *http.Request) {
	// Read the raw body first — needed for signature verification before parsing.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB max
	if err != nil {
		response.BadRequest(w, "READ_ERROR", "failed to read request body")
		return
	}

	// Verify the HUB2 HMAC-SHA256 signature.
	sig := r.Header.Get("X-Hub2-Signature")
	if !hub2client.VerifySignature(body, sig, h.hub2Secret) {
		h.logger.Warn("hub2 webhook: invalid signature",
			zap.String("sig_header", sig),
			zap.String("remote_addr", r.RemoteAddr),
		)
		response.Forbidden(w, "invalid webhook signature")
		return
	}

	var payload hub2client.WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		response.BadRequest(w, "PARSE_ERROR", "invalid JSON payload")
		return
	}

	h.logger.Info("hub2 webhook received",
		zap.String("reference", payload.Reference),
		zap.String("status", payload.Status),
		zap.String("type", payload.Type),
		zap.String("phone", payload.Phone),
		zap.Float64("amount", payload.Amount),
		zap.String("currency", payload.Currency),
	)

	if err := h.svc.HUB2.HandleWebhook(r.Context(), payload); err != nil {
		h.logger.Error("hub2 webhook processing failed",
			zap.String("reference", payload.Reference),
			zap.Error(err),
		)
		response.InternalError(w)
		return
	}

	response.OKWithMessage(w, "webhook processed", nil)
}
