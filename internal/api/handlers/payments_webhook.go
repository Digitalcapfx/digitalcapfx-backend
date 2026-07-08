package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type PaymentsWebhookHandler struct {
	svc    *services.Services
	secret string
	logger *zap.Logger
}

func NewPaymentsWebhookHandler(svc *services.Services, secret string, logger *zap.Logger) *PaymentsWebhookHandler {
	return &PaymentsWebhookHandler{svc: svc, secret: secret, logger: logger}
}

type PaymentsWebhookPayload struct {
	Event string              `json:"event"`
	Data  PaymentsWebhookData `json:"data"`
}

type PaymentsWebhookData struct {
	CustomerID   string `json:"customer_id"`
	Network      string `json:"network"`
	Address      string `json:"address"`
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
	TxHash       string `json:"tx_hash"`
	Status       string `json:"status"`
	SafeToCredit bool   `json:"safe_to_credit"`
}

// Receive godoc
//
//	@Summary      Rach WaaS Webhook
//	@Description  Handles transaction updates (detected/confirmed deposits) pushed by the Payments WaaS microservice.
//	@Tags         webhooks
//	@Accept       json
//	@Produce      json
//	@Param        X-Webhook-Signature  header    string                  true  "HMAC-SHA256 signature: hex"
//	@Param        body                 body      PaymentsWebhookPayload  true  "Webhook payload"
//	@Success      200                  {object}  MessageResponse
//	@Failure      400                  {object}  ErrorResponse
//	@Router       /webhooks/payments [post]
func (h *PaymentsWebhookHandler) Receive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		response.BadRequest(w, "READ_ERROR", "failed to read request body")
		return
	}

	// Verify HMAC-SHA256 signature
	if h.secret != "" {
		sig := r.Header.Get("X-Webhook-Signature")
		if !verifyPaymentsWebhookSignature(sig, body, h.secret) {
			h.logger.Warn("payments webhook: invalid signature",
				zap.String("sig", sig),
			)
			response.Unauthorized(w, "invalid webhook signature")
			return
		}
	}

	var payload PaymentsWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		response.BadRequest(w, "INVALID_PAYLOAD", "invalid JSON payload")
		return
	}

	h.logger.Info("payments webhook received",
		zap.String("event", payload.Event),
		zap.String("address", payload.Data.Address),
		zap.String("network", payload.Data.Network),
		zap.String("amount", payload.Data.Amount),
		zap.String("currency", payload.Data.Currency),
		zap.String("tx_hash", payload.Data.TxHash),
	)

	switch strings.ToLower(payload.Event) {
	case "wallet.deposit.detected":
		// Just log it — we don't credit yet, waiting for confirmation
		h.logger.Info("deposit detected (pending confirmation)",
			zap.String("tx_hash", payload.Data.TxHash),
			zap.String("address", payload.Data.Address),
		)
	case "wallet.deposit.confirmed":
		if err := h.creditDeposit(r.Context(), payload.Data); err != nil {
			h.logger.Error("failed to credit deposit",
				zap.String("tx_hash", payload.Data.TxHash),
				zap.String("address", payload.Data.Address),
				zap.Error(err),
			)
			// Return 200 so webhook isn't retried for business logic failures
		}
	default:
		h.logger.Info("payments webhook: unhandled event", zap.String("event", payload.Event))
	}

	response.OKWithMessage(w, "received", nil)
}

// creditDeposit credits a confirmed on-chain deposit to the owning user's
// account and sends a notification. Called from Receive on
// "wallet.deposit.confirmed" events.
func (h *PaymentsWebhookHandler) creditDeposit(ctx context.Context, data PaymentsWebhookData) error {
	if data.Address == "" || data.Amount == "" || data.Currency == "" {
		return fmt.Errorf("missing required deposit fields (address/amount/currency)")
	}

	// Normalize currency (e.g. "USDC_POLYGON" → "USDC")
	currency := normalizeCurrency(data.Currency)

	amount, err := strconv.ParseFloat(data.Amount, 64)
	if err != nil || amount <= 0 {
		return fmt.Errorf("invalid amount %q: %w", data.Amount, err)
	}

	// Convert to smallest unit (multiply by 100 to get cents/base units)
	amountInt := int64(amount * 100)

	// Find the user by derived wallet address.
	wallet, err := h.svc.Wallet.GetWalletByAddress(ctx, data.Address)
	if err != nil {
		return fmt.Errorf("wallet not found for address %s: %w", data.Address, err)
	}

	if err := h.svc.Wallet.CreditWaasDeposit(ctx, wallet.UserID, currency, amountInt, data.TxHash); err != nil {
		return fmt.Errorf("credit balance for user %s: %w", wallet.UserID, err)
	}

	h.logger.Info("payments webhook: deposit credited",
		zap.String("user_id", wallet.UserID.String()),
		zap.String("currency", currency),
		zap.Int64("amount_cents", amountInt),
		zap.String("tx_hash", data.TxHash),
	)
	h.svc.Notifications.Create(ctx, services.CreateNotificationInput{
		UserID: wallet.UserID,
		Type:   services.NotifDepositConfirmed,
		Title:  fmt.Sprintf("Deposit Confirmed: %s %s", data.Amount, currency),
		Body:   fmt.Sprintf("Your %s deposit of %s has been confirmed and credited to your account.", currency, data.Amount),
		Metadata: map[string]string{
			"tx_hash":  data.TxHash,
			"network":  data.Network,
			"currency": currency,
		},
	})
	return nil
}

func normalizeCurrency(raw string) string {
	// e.g. "USDC_POLYGON" → "USDC", "USDT_BSC" → "USDT", "USDC" → "USDC"
	if idx := strings.Index(raw, "_"); idx != -1 {
		return raw[:idx]
	}
	return strings.ToUpper(raw)
}

func verifyPaymentsWebhookSignature(signature string, body []byte, secret string) bool {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(body)
	expected := hex.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expected))
}