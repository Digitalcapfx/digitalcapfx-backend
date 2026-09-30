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

// CaasWebhookHandler receives webhooks from Rach CaaS (the Stablecoin / SCW
// settlement service) and verifies them against the shared CAAS_WEBHOOK_SECRET.
type CaasWebhookHandler struct {
	svc    *services.Services
	secret string
	logger *zap.Logger
}

func NewCaasWebhookHandler(svc *services.Services, secret string, logger *zap.Logger) *CaasWebhookHandler {
	return &CaasWebhookHandler{svc: svc, secret: secret, logger: logger}
}

// CaasWebhookPayload is the FLAT Rach CaaS webhook body. Every event carries
// `event`; the remaining fields depend on the event type (all top-level — there
// is no nested "data" wrapper). Amounts are 6dp decimal strings; tx_hash is the
// real on-chain EVM hash; timestamps are RFC3339 UTC.
//
// Events: transfer.received, transfer.settled, transfer.failed, swap.settled,
// swap.failed, user.linked, user.phone_changed.
type CaasWebhookPayload struct {
	Event string `json:"event"`

	// transfer.* — recipient is addressed by wallet, amount is amount_usdc.
	TransferID      string `json:"transfer_id"`
	RecipientWallet string `json:"recipient_wallet"`
	AmountUSDC      string `json:"amount_usdc"`
	Token           string `json:"token"`

	// swap.* fields.
	SwapID   string `json:"swap_id"`
	TokenIn  string `json:"token_in"`
	TokenOut string `json:"token_out"`
	AmountIn string `json:"amount_in"`

	// Common terminal fields.
	Status string `json:"status"` // SETTLED | FAILED (transfer/swap)
	Reason string `json:"reason"` // present on *.failed
	TxHash string `json:"tx_hash"`

	// Account / membership events.
	WalletAddress       string `json:"wallet_address"`
	NewMerchantID       string `json:"new_merchant_id"`
	ChangedByMerchantID string `json:"changed_by_merchant_id"`

	// Timestamps (RFC3339 UTC).
	SettledAt string `json:"settled_at"`
	UpdatedAt string `json:"updated_at"`
	LinkedAt  string `json:"linked_at"`
	ChangedAt string `json:"changed_at"`
}

// Receive godoc
//
//	@Summary      Rach CaaS Webhook (Stablecoin / USDC)
//	@Description  Receiver for Rach CaaS transaction and account events. Each delivery is an HTTP POST with a flat JSON body and header `X-Rach-Signature: sha256=<hex>` — the hex HMAC-SHA256 of the raw request body keyed with the CaaS webhook secret (CAAS_WEBHOOK_SECRET), which is SEPARATE from the WaaS secret. Handles transfer.received (credit → notify + email the recipient, resolved by wallet address), transfer.settled/failed and swap.settled/failed (update the recorded transaction/swap + notify the initiator), and the user.linked / user.phone_changed lifecycle events. Always returns 200 on business-logic issues so the provider does not retry an already-accepted delivery.
//	@Tags         webhooks
//	@Accept       json
//	@Produce      json
//	@Param        X-Rach-Signature  header    string              true  "sha256=<hex HMAC-SHA256(rawBody, CaaS secret)>"
//	@Param        body              body      CaasWebhookPayload  true  "Webhook payload"
//	@Success      200               {object}  MessageResponse
//	@Failure      401               {object}  ErrorResponse
//	@Router       /webhooks/caas [post]
func (h *CaasWebhookHandler) Receive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB cap
	if err != nil {
		response.BadRequest(w, "READ_ERROR", "failed to read request body")
		return
	}

	// Verify the HMAC-SHA256 signature when a secret is configured. Rach signs as
	// `X-Rach-Signature: sha256=<hex>` over the raw body; we also tolerate a few
	// alternate header names / encodings as a hardening fallback and log the
	// received headers on mismatch.
	if h.secret != "" {
		if !verifyCaasWebhookSignature(r, body, []byte(h.secret)) {
			h.logger.Warn("caas webhook: signature verification failed",
				zap.Strings("x_headers", collectSignatureHeaders(r)))
			response.Unauthorized(w, "invalid webhook signature")
			return
		}
	}

	var p CaasWebhookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		response.BadRequest(w, "INVALID_PAYLOAD", "invalid JSON payload")
		return
	}

	h.logger.Info("caas webhook received",
		zap.String("event", p.Event),
		zap.String("status", p.Status),
		zap.String("transfer_id", p.TransferID),
		zap.String("swap_id", p.SwapID),
		zap.String("tx_hash", p.TxHash),
	)

	// Route on event type. Business-logic failures still ack with 200 so the
	// provider does not retry a delivery we've already accepted.
	switch strings.ToLower(strings.TrimSpace(p.Event)) {
	case "transfer.received":
		h.onTransferReceived(r, &p)
	case "transfer.settled", "transfer.failed":
		h.onTransferFinalised(r, &p)
	case "swap.settled", "swap.failed":
		h.onSwapFinalised(r, &p)
	case "user.linked", "user.phone_changed":
		h.logger.Info("caas account event",
			zap.String("event", p.Event), zap.String("wallet_address", p.WalletAddress))
	default:
		h.logger.Info("caas webhook: unhandled event", zap.String("event", p.Event))
	}

	response.OKWithMessage(w, "received", nil)
}

// onTransferReceived notifies (in-app + push + email) the customer whose SCW
// just received stablecoin. The recipient is identified by wallet address. The
// balance is fetched live elsewhere, so this is a signal only. Customer-facing
// label is USDC (name "Stablecoin").
func (h *CaasWebhookHandler) onTransferReceived(r *http.Request, p *CaasWebhookPayload) {
	userID, ok := h.svc.CaaS.WalletOwner(r.Context(), p.RecipientWallet)
	if !ok {
		h.logger.Info("caas transfer.received for unknown wallet",
			zap.String("recipient_wallet", p.RecipientWallet), zap.String("transfer_id", p.TransferID))
		return
	}
	token := firstNonEmpty(p.Token, "USDC")
	amount := p.AmountUSDC
	h.svc.Notifications.Create(r.Context(), services.CreateNotificationInput{
		UserID: userID,
		Type:   services.NotifCryptoRecvd,
		Title:  fmt.Sprintf("%s Received: %s %s", token, amount, token),
		Body:   fmt.Sprintf("Your Stablecoin (%s) account has been credited with %s %s.", token, amount, token),
		Metadata: map[string]string{
			"transfer_id": p.TransferID, "tx_hash": p.TxHash,
			"symbol": token, "amount": amount, "source": "caas",
		},
		EmailSubject: fmt.Sprintf("You received %s %s", amount, token),
		EmailHTML:    caasReceivedEmailHTML(amount, token, p.TxHash, h.svc.Notifications.BaseURL()),
	})
}

// onTransferFinalised updates a recorded outbound transfer to its terminal state
// and notifies the sender.
func (h *CaasWebhookHandler) onTransferFinalised(r *http.Request, p *CaasWebhookPayload) {
	tx, err := h.svc.CaaS.ApplyTransferWebhook(r.Context(), p.TransferID, "", p.Status, p.TxHash)
	if err != nil {
		h.logger.Warn("caas transfer webhook: could not match local transfer",
			zap.String("event", p.Event), zap.String("transfer_id", p.TransferID), zap.Error(err))
		return
	}
	settled := strings.EqualFold(p.Status, "SETTLED") || strings.ToLower(strings.TrimSpace(p.Event)) == "transfer.settled"
	title, body := "Transfer completed", fmt.Sprintf("Your transfer of %s %s has completed.", tx.Amount, tx.Token)
	if !settled {
		title = "Transfer failed"
		body = fmt.Sprintf("Your transfer of %s %s could not be completed.", tx.Amount, tx.Token)
		if p.Reason != "" {
			body += " Reason: " + p.Reason
		}
	}
	h.svc.Notifications.Create(r.Context(), services.CreateNotificationInput{
		UserID: tx.SenderUserID,
		Type:   services.NotifCryptoSent,
		Title:  title,
		Body:   body,
		Metadata: map[string]string{
			"transfer_id": p.TransferID, "reference": tx.Reference, "token": tx.Token,
			"amount": tx.Amount, "status": tx.Status, "tx_hash": derefStr(tx.TxHash, ""),
			"reason": p.Reason, "source": "caas",
		},
	})
}

// onSwapFinalised updates a recorded swap to its terminal state and notifies the owner.
func (h *CaasWebhookHandler) onSwapFinalised(r *http.Request, p *CaasWebhookPayload) {
	// swap.* payloads carry amount_in + tx_hash but not amount_out; leave amount_out unset.
	swap, err := h.svc.CaaS.ApplySwapWebhook(r.Context(), p.SwapID, p.Status, "", p.TxHash)
	if err != nil {
		h.logger.Warn("caas swap webhook: could not match local swap",
			zap.String("event", p.Event), zap.String("swap_id", p.SwapID), zap.Error(err))
		return
	}
	settled := swap.Status == "settled"
	notifType := services.NotifSwapFailed
	title := "Swap failed"
	body := fmt.Sprintf("Your swap of %s %s to %s could not be completed.", swap.AmountIn, swap.TokenIn, swap.TokenOut)
	if !settled && p.Reason != "" {
		body += " Reason: " + p.Reason
	}
	if settled {
		notifType = services.NotifSwapSettled
		title = "Swap completed"
		body = fmt.Sprintf("Your swap of %s %s to %s is complete.", swap.AmountIn, swap.TokenIn, swap.TokenOut)
	}
	h.svc.Notifications.Create(r.Context(), services.CreateNotificationInput{
		UserID: swap.UserID,
		Type:   notifType,
		Title:  title,
		Body:   body,
		Metadata: map[string]string{
			"swap_id": p.SwapID, "reference": swap.Reference,
			"token_in": swap.TokenIn, "token_out": swap.TokenOut, "amount_in": swap.AmountIn,
			"status": swap.Status, "tx_hash": derefStr(swap.TxHash, ""), "reason": p.Reason, "source": "caas",
		},
	})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// caasReceivedEmailHTML renders the incoming-transfer email for the recipient.
func caasReceivedEmailHTML(amount, token, txHash, baseURL string) string {
	ref := ""
	if txHash != "" {
		ref = fmt.Sprintf(`<p style="color:#888;font-size:12px">Transaction: %s</p>`, txHash)
	}
	return fmt.Sprintf(`<div style="font-family:Arial,sans-serif;max-width:520px;margin:0 auto">
  <h2 style="color:#111">Stablecoin received</h2>
  <p>You just received <strong>%s %s</strong>.</p>
  <p>The funds are already available in your Stablecoin (%s) balance.</p>
  %s
  <p style="margin-top:24px"><a href="%s" style="background:#111;color:#fff;padding:10px 18px;border-radius:6px;text-decoration:none">Open DigitalFX</a></p>
</div>`, amount, token, token, ref, baseURL)
}

// signatureHeaderNames — Rach signs as X-Rach-Signature; the others are accepted
// as a hardening fallback in case the outbound header name varies.
var signatureHeaderNames = []string{
	"X-Rach-Signature", "X-Webhook-Signature", "X-Signature",
	"X-Hub-Signature-256", "Webhook-Signature", "X-Caas-Signature",
}

// verifyCaasWebhookSignature returns true if any candidate signature header
// matches the HMAC-SHA256 of the raw body, in hex (the documented form) or
// base64, with an optional "sha256=" / "sha256:" prefix.
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

// collectSignatureHeaders returns the signature-ish request header names (for
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
