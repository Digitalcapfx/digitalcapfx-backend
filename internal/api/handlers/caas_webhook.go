package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

// CaasWebhookHandler receives webhooks from Rach CaaS (the instant USD / SCW
// account service) and verifies them against the shared CAAS_WEBHOOK_SECRET.
type CaasWebhookHandler struct {
	svc    *services.Services
	secret string
	logger *zap.Logger
}

func NewCaasWebhookHandler(svc *services.Services, secret string, logger *zap.Logger) *CaasWebhookHandler {
	return &CaasWebhookHandler{svc: svc, secret: secret, logger: logger}
}

// CaasWebhookPayload is a permissive shape covering the fields CaaS sends. The
// customer is identified by phone (CaaS keys accounts by phone / blind index).
type CaasWebhookPayload struct {
	Event string `json:"event"`
	Data  struct {
		Phone      string `json:"phone"`
		BlindIndex string `json:"blind_index"`
		Amount     string `json:"amount"`
		Currency   string `json:"currency"`
		TxHash     string `json:"tx_hash"`
		Reference  string `json:"reference"`
		Status     string `json:"status"`
	} `json:"data"`
}

// Receive godoc
//
//	@Summary      Rach CaaS Webhook
//	@Description  Receives account/settlement events from Rach CaaS (instant USD account). Verified via HMAC-SHA256 over the raw body in the X-Webhook-Signature header, using the CaaS webhook secret. On a credit/deposit event the owning user is notified.
//	@Tags         webhooks
//	@Accept       json
//	@Produce      json
//	@Param        X-Webhook-Signature  header    string              true  "HMAC-SHA256 signature (hex)"
//	@Param        body                 body      CaasWebhookPayload  true  "Webhook payload"
//	@Success      200                  {object}  MessageResponse
//	@Failure      401                  {object}  ErrorResponse
//	@Router       /webhooks/caas [post]
func (h *CaasWebhookHandler) Receive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB cap
	if err != nil {
		response.BadRequest(w, "READ_ERROR", "failed to read request body")
		return
	}

	// Verify the HMAC-SHA256 signature when a secret is configured. The CaaS
	// swagger documents the {url, secret} registration but not the outbound
	// signing convention, so we accept the signature under several common header
	// names and encodings (hex/base64, optional "sha256=" prefix). On a mismatch
	// we log the received X-* headers so the exact scheme can be confirmed from
	// the first real delivery.
	if h.secret != "" {
		if !verifyCaasWebhookSignature(r, body, []byte(h.secret)) {
			h.logger.Warn("caas webhook: signature verification failed",
				zap.Strings("x_headers", collectSignatureHeaders(r)))
			response.Unauthorized(w, "invalid webhook signature")
			return
		}
	}

	var payload CaasWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		response.BadRequest(w, "INVALID_PAYLOAD", "invalid JSON payload")
		return
	}

	h.logger.Info("caas webhook received",
		zap.String("event", payload.Event),
		zap.String("status", payload.Data.Status),
		zap.String("amount", payload.Data.Amount),
		zap.String("currency", payload.Data.Currency),
		zap.String("reference", payload.Data.Reference),
	)

	// Notify the customer on a successful credit/deposit/settlement. The CaaS
	// balance itself is authoritative and fetched live, so this is a signal only.
	ev := strings.ToLower(payload.Event + " " + payload.Data.Status)
	credited := strings.Contains(ev, "credit") || strings.Contains(ev, "deposit") ||
		strings.Contains(ev, "settle") || strings.Contains(ev, "success")

	if credited && payload.Data.Phone != "" {
		currency := payload.Data.Currency
		if currency == "" {
			currency = "USDC"
		}
		h.svc.Notifications.CreateForPhone(r.Context(), payload.Data.Phone, services.CreateNotificationInput{
			Type:  services.NotifDepositConfirmed,
			Title: fmt.Sprintf("USD Account Credited: %s %s", payload.Data.Amount, currency),
			Body:  fmt.Sprintf("Your instant USD account has been credited with %s %s.", payload.Data.Amount, currency),
			Metadata: map[string]string{
				"tx_hash":   payload.Data.TxHash,
				"currency":  currency,
				"reference": payload.Data.Reference,
				"source":    "caas",
			},
		})
	}

	response.OKWithMessage(w, "received", nil)
}

// signatureHeaderNames are the header names webhook providers commonly use for
// the HMAC signature. We check all of them since the CaaS outbound scheme isn't
// documented in swagger.
var signatureHeaderNames = []string{
	"X-Webhook-Signature", "X-Signature", "X-Rach-Signature",
	"X-Hub-Signature-256", "Webhook-Signature", "X-Caas-Signature",
}

// verifyCaasWebhookSignature returns true if any candidate signature header
// matches the HMAC-SHA256 of the raw body, in hex or base64, with an optional
// "sha256=" / "sha256:" prefix.
func verifyCaasWebhookSignature(r *http.Request, body, secret []byte) bool {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sum := mac.Sum(nil)
	expectedHex := hex.EncodeToString(sum)
	expectedB64 := base64.StdEncoding.EncodeToString(sum)

	for _, name := range signatureHeaderNames {
		got := r.Header.Get(name)
		if got == "" {
			continue
		}
		got = strings.TrimSpace(got)
		got = strings.TrimPrefix(got, "sha256=")
		got = strings.TrimPrefix(got, "sha256:")
		if hmac.Equal([]byte(got), []byte(expectedHex)) || hmac.Equal([]byte(got), []byte(expectedB64)) {
			return true
		}
	}
	return false
}

// collectSignatureHeaders returns the signature-ish request headers (for
// debugging an unknown provider scheme). Values are not logged — only presence.
func collectSignatureHeaders(r *http.Request) []string {
	var found []string
	for name := range r.Header {
		if strings.HasPrefix(strings.ToLower(name), "x-") || strings.Contains(strings.ToLower(name), "signature") {
			found = append(found, name)
		}
	}
	return found
}
