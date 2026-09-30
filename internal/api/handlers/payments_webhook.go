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

// PaymentsWebhookData mirrors the Rach WaaS deposit webhook `data` object. A
// deposit arrives as a two-stage flow (detected → confirmed) plus a later
// hash-resolution event:
//
//   - wallet.deposit.detected     — records a pending deposit; does NOT credit.
//   - wallet.deposit.confirmed    — credits the balance, but only when
//     safe_to_credit is true.
//   - wallet.deposit.hash_resolved — backfills the real on-chain tx_hash later.
//
// Only customer_id is guaranteed present (a partial event won't 400); a genuine
// deposit carries the full set. deposit_id is the idempotency key — dedup on it,
// NOT tx_hash. tx_hash is a provisional placeholder ("pending_<ts>_<addr>") on
// detected/confirmed and only becomes the real chain hash on hash_resolved.
// currency is the network code (e.g. "POL") or token symbol ("USDT"/"USDC");
// amount is a decimal STRING (e.g. "10").
type PaymentsWebhookData struct {
	CustomerID    string `json:"customer_id"`
	DepositID     int64  `json:"deposit_id"`
	Network       string `json:"network"`
	Address       string `json:"address"`
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
	TxHash        string `json:"tx_hash"`
	HashPending   bool   `json:"hash_pending"`
	Confirmations int    `json:"confirmations"`
	Status        string `json:"status"`
	DetectedAt    string `json:"detected_at"`
	ConfirmedAt   string `json:"confirmed_at"`
	SafeToCredit  bool   `json:"safe_to_credit"`
}

// Receive godoc
//
//	@Summary      Rach WaaS Webhook (crypto deposits)
//	@Description  Receiver for Rach WaaS on-chain deposit events, delivered as a two-stage flow plus a later hash resolution: `wallet.deposit.detected` (seen on-chain — records a pending deposit, does NOT credit), `wallet.deposit.confirmed` (credits the owning user's local ledger, but only when `safe_to_credit` is true), and `wallet.deposit.hash_resolved` (backfills the real on-chain tx_hash). The owner is resolved by derived address; crediting is idempotent, keyed on `deposit_id` (NOT tx_hash, which is a provisional placeholder until hash_resolved), so a re-delivered event neither double-credits nor re-notifies. The WaaS balance API remains authoritative. Signature: header `X-Webhook-Signature` = hex HMAC-SHA256 of the raw request body, keyed with the WaaS webhook secret (PAYMENTS_WEBHOOK_SECRET). Always returns 200 on business-logic issues to avoid provider retries.
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

	switch strings.ToLower(strings.TrimSpace(payload.Event)) {
	case "wallet.deposit.detected":
		// Pending only — never credited here. The confirmed event (gated on
		// safe_to_credit) is what moves money.
		h.logger.Info("waas deposit detected (pending confirmation)",
			zap.Int64("deposit_id", payload.Data.DepositID),
			zap.String("address", payload.Data.Address),
			zap.String("amount", payload.Data.Amount),
			zap.String("currency", payload.Data.Currency),
		)
	case "wallet.deposit.confirmed":
		// Never errors: gates on safe_to_credit, credits idempotently by
		// deposit_id, notifies the owner only on first processing.
		h.handleConfirmedDeposit(r.Context(), payload.Data)
	case "wallet.deposit.hash_resolved":
		// The real on-chain hash backfills here (same deposit_id). We don't key
		// anything on tx_hash and the WaaS balance API is authoritative, so this
		// is informational — log it for traceability.
		h.logger.Info("waas deposit hash resolved",
			zap.Int64("deposit_id", payload.Data.DepositID),
			zap.String("tx_hash", payload.Data.TxHash),
		)
	default:
		h.logger.Info("payments webhook: unhandled event", zap.String("event", payload.Event))
	}

	response.OKWithMessage(w, "received", nil)
}

// handleConfirmedDeposit processes a confirmed on-chain WaaS deposit. It only
// credits when safe_to_credit is true, resolves the owning user by derived
// address, and mirror-credits the local ledger idempotently keyed on deposit_id
// — so a re-delivered event neither double-credits nor re-notifies. It never
// returns an error: the WaaS balance API is authoritative and the webhook must
// always ack with 200.
func (h *PaymentsWebhookHandler) handleConfirmedDeposit(ctx context.Context, data PaymentsWebhookData) {
	// The gate: crediting only happens on a confirmed event with safe_to_credit.
	if !data.SafeToCredit {
		h.logger.Info("waas deposit confirmed but not yet safe_to_credit — skipping credit",
			zap.Int64("deposit_id", data.DepositID), zap.String("address", data.Address))
		return
	}
	if data.Address == "" {
		h.logger.Warn("payments webhook: confirmed deposit missing address", zap.Int64("deposit_id", data.DepositID))
		return
	}
	// deposit_id is the idempotency key; without it we cannot safely dedup.
	if data.DepositID == 0 {
		h.logger.Warn("payments webhook: confirmed deposit missing deposit_id — skipping credit",
			zap.String("address", data.Address))
		return
	}

	// Resolve the owner by the derived on-chain address.
	wallet, err := h.svc.Wallet.GetWalletByAddress(ctx, data.Address)
	if err != nil {
		h.logger.Error("payments webhook: wallet not found for address",
			zap.String("address", data.Address), zap.Int64("deposit_id", data.DepositID), zap.Error(err))
		return
	}

	// currency is a network code (POL/BTC/…) or token symbol (USDT/USDC).
	currency := normalizeCurrency(data.Currency)
	amount, perr := strconv.ParseFloat(data.Amount, 64)
	if perr != nil || amount <= 0 {
		h.logger.Warn("payments webhook: confirmed deposit invalid amount",
			zap.String("amount", data.Amount), zap.Int64("deposit_id", data.DepositID))
		return
	}

	// Idempotent mirror-credit keyed on deposit_id. Returns false when this
	// deposit was already processed (re-delivery) or the user has no matching
	// local account — either way we must not notify.
	eventID := fmt.Sprintf("deposit:%d", data.DepositID)
	credited, cerr := h.svc.Wallet.CreditWaasDepositOnce(ctx, wallet.UserID, currency, amount, eventID)
	if cerr != nil {
		h.logger.Warn("payments webhook: local mirror-credit skipped",
			zap.String("currency", currency), zap.String("user_id", wallet.UserID.String()),
			zap.Int64("deposit_id", data.DepositID), zap.Error(cerr))
		return
	}
	if !credited {
		h.logger.Info("waas deposit already processed (idempotent no-op)",
			zap.Int64("deposit_id", data.DepositID), zap.String("user_id", wallet.UserID.String()))
		return
	}

	// First time we've credited this deposit → notify the owner exactly once.
	h.svc.Notifications.Create(ctx, services.CreateNotificationInput{
		UserID: wallet.UserID,
		Type:   services.NotifDepositConfirmed,
		Title:  fmt.Sprintf("Crypto Deposit Confirmed: %s %s", data.Amount, currency),
		Body:   fmt.Sprintf("Your %s deposit of %s %s on %s has been confirmed on-chain.", currency, data.Amount, currency, data.Network),
		Metadata: map[string]string{
			"deposit_id": strconv.FormatInt(data.DepositID, 10),
			"tx_hash":    data.TxHash,
			"network":    data.Network,
			"currency":   currency,
			"amount":     data.Amount,
			"source":     "waas",
		},
	})

	h.logger.Info("payments webhook: confirmed deposit credited",
		zap.String("user_id", wallet.UserID.String()),
		zap.String("currency", currency),
		zap.String("amount", data.Amount),
		zap.Int64("deposit_id", data.DepositID),
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