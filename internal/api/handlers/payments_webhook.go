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

// PaymentsWebhookData mirrors the Rach WaaS deposit webhook `data` object exactly
// (payments confirmation_service.go / wallet_monitoring.go). currency is the
// network code (e.g. "POL", "BTC") for native coins or the token symbol
// ("USDT", "USDC"); amount is a high-precision decimal string.
type PaymentsWebhookData struct {
	CustomerID    string `json:"customer_id"`
	Network       string `json:"network"`
	Address       string `json:"address"`
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
	TxHash        string `json:"tx_hash"`
	Confirmations int    `json:"confirmations"`
	Status        string `json:"status"`
	DetectedAt    string `json:"detected_at"`
	ConfirmedAt   string `json:"confirmed_at"`
	SafeToCredit  bool   `json:"safe_to_credit"`
}

// Receive godoc
//
//	@Summary      Rach WaaS Webhook (crypto deposits)
//	@Description  Receiver for Rach WaaS on-chain deposit events. Fires `wallet.deposit.detected` (seen on-chain, not credited) and `wallet.deposit.confirmed` (block-depth/settlement threshold met). On a confirmed event the owning user (resolved by derived address) is notified and the deposit is mirrored best-effort into their local ledger; the WaaS balance API remains authoritative. Signature: header `X-Webhook-Signature` = hex HMAC-SHA256 of the raw request body, keyed with the WaaS webhook secret (PAYMENTS_WEBHOOK_SECRET). Always returns 200 on business-logic issues to avoid provider retries.
//	@Tags         webhooks
//	@Accept       json
//	@Produce      json
//	@Param        X-Webhook-Signature  header    string                  true  "hex HMAC-SHA256(rawBody, secret)"
//	@Param        X-Webhook-Event      header    string                  false "Event name (e.g. wallet.deposit)"
//	@Param        body                 body      PaymentsWebhookPayload  true  "Webhook payload"
//	@Success      200                  {object}  MessageResponse
//	@Failure      400                  {object}  ErrorResponse
//	@Failure      401                  {object}  ErrorResponse
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
		// Never errors: notifies the owner and mirror-credits best-effort.
		h.handleConfirmedDeposit(r.Context(), payload.Data)
	default:
		h.logger.Info("payments webhook: unhandled event", zap.String("event", payload.Event))
	}

	response.OKWithMessage(w, "received", nil)
}

// handleConfirmedDeposit processes a confirmed on-chain WaaS deposit: it resolves
// the owning user by derived address, ALWAYS notifies them, and mirror-credits
// their local ledger best-effort. It never returns an error — the WaaS balance
// API is authoritative, and the webhook must always ack with 200.
func (h *PaymentsWebhookHandler) handleConfirmedDeposit(ctx context.Context, data PaymentsWebhookData) {
	if data.Address == "" {
		h.logger.Warn("payments webhook: confirmed deposit missing address", zap.String("tx_hash", data.TxHash))
		return
	}

	// Resolve the owner by the derived on-chain address.
	wallet, err := h.svc.Wallet.GetWalletByAddress(ctx, data.Address)
	if err != nil {
		h.logger.Error("payments webhook: wallet not found for address",
			zap.String("address", data.Address), zap.String("tx_hash", data.TxHash), zap.Error(err))
		return
	}

	// currency is a network code (POL/BTC/…) or token symbol (USDT/USDC).
	currency := normalizeCurrency(data.Currency)

	// Always notify the owner that their on-chain deposit confirmed.
	h.svc.Notifications.Create(ctx, services.CreateNotificationInput{
		UserID: wallet.UserID,
		Type:   services.NotifDepositConfirmed,
		Title:  fmt.Sprintf("Crypto Deposit Confirmed: %s %s", data.Amount, currency),
		Body:   fmt.Sprintf("Your %s deposit of %s %s on %s has been confirmed on-chain.", currency, data.Amount, currency, data.Network),
		Metadata: map[string]string{
			"tx_hash":  data.TxHash,
			"network":  data.Network,
			"currency": currency,
			"amount":   data.Amount,
			"source":   "waas",
		},
	})

	// Best-effort mirror into a matching local ledger account if one exists.
	// Skipped silently when the user has no account for this currency — the WaaS
	// balance API remains the source of truth for crypto holdings.
	if amount, perr := strconv.ParseFloat(data.Amount, 64); perr == nil && amount > 0 {
		amountInt := int64(amount * 100)
		if cerr := h.svc.Wallet.CreditWaasDeposit(ctx, wallet.UserID, currency, amountInt, data.TxHash); cerr != nil {
			h.logger.Warn("payments webhook: local mirror-credit skipped",
				zap.String("currency", currency), zap.String("user_id", wallet.UserID.String()), zap.Error(cerr))
		}
	}

	h.logger.Info("payments webhook: confirmed deposit processed",
		zap.String("user_id", wallet.UserID.String()),
		zap.String("currency", currency),
		zap.String("amount", data.Amount),
		zap.String("tx_hash", data.TxHash),
	)
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