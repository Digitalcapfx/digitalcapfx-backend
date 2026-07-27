package nomba

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
)

// Webhook event types (event_type field).
const (
	EventPaymentSuccess  = "payment_success"  // virtual-account credit, card, PayByTransfer
	EventPaymentFailed   = "payment_failed"   //
	EventPaymentReversal = "payment_reversal" //
	EventPayoutSuccess   = "payout_success"   // outgoing transfer / bill payment succeeded
	EventPayoutFailed    = "payout_failed"    //
	EventPayoutRefund    = "payout_refund"    //
)

// Signature headers.
const (
	SignatureHeader          = "nomba-signature"
	SignatureAlgorithmHeader = "nomba-signature-algorithm"
	TimestampHeader          = "nomba-timestamp"
)

// WebhookEvent is a parsed Nomba webhook. It is a permissive shape covering the
// fields DigitalFX cares about (a virtual-account credit identifies the customer
// by the alias/virtual account number).
type WebhookEvent struct {
	EventType string `json:"event_type"`
	RequestID string `json:"requestId"`
	Data      struct {
		Merchant struct {
			WalletID      string  `json:"walletId"`
			WalletBalance float64 `json:"walletBalance"`
			UserID        string  `json:"userId"`
		} `json:"merchant"`
		Transaction struct {
			// The virtual (alias) account number that was credited — this is how
			// we map an inbound NGN credit back to the owning DigitalFX account.
			AliasAccountNumber string  `json:"aliasAccountNumber"`
			TransactionID      string  `json:"transactionId"`
			ID                 string  `json:"id"`
			Type               string  `json:"type"`
			TransactionAmount  float64 `json:"transactionAmount"`
			Time               string  `json:"time"`
			ResponseCode       string  `json:"responseCode"`
			MerchantTxRef      string  `json:"merchantTxRef"`
		} `json:"transaction"`
		Customer struct {
			SenderName    string `json:"senderName"`
			AccountNumber string `json:"accountNumber"`
			BankCode      string `json:"bankCode"`
		} `json:"customer"`
	} `json:"data"`
}

// TxID returns the transaction id regardless of which field Nomba populated.
func (e *WebhookEvent) TxID() string {
	if e.Data.Transaction.TransactionID != "" {
		return e.Data.Transaction.TransactionID
	}
	return e.Data.Transaction.ID
}

// ParseWebhook unmarshals a raw webhook body.
func ParseWebhook(body []byte) (*WebhookEvent, error) {
	var ev WebhookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, err
	}
	return &ev, nil
}

// VerifyWebhookSignature verifies a Nomba webhook against the configured signing
// secret. Nomba signs a colon-delimited string of event fields (NOT the raw body):
//
//	Base64(HMAC-SHA256(eventType:requestId:userId:walletId:txId:txType:txTime:responseCode:timestamp, secret))
//
// carried in the `nomba-signature` header. The exact field order is taken from
// the Nomba webhook docs; because the precise composition can only be confirmed
// against a real delivery, the handler logs a mismatch (rather than silently
// trusting) so the scheme can be verified from the first live webhook.
//
// Returns false if no signature header is present or the secret is empty.
func VerifyWebhookSignature(headers http.Header, ev *WebhookEvent, secret string) bool {
	if secret == "" || ev == nil {
		return false
	}
	got := strings.TrimSpace(headers.Get(SignatureHeader))
	if got == "" {
		return false
	}
	expected := computeSignature(ev, headers.Get(TimestampHeader), secret)

	// Tolerate an optional "sha256=" prefix some providers add.
	got = strings.TrimPrefix(got, "sha256=")
	return hmac.Equal([]byte(got), []byte(expected))
}

// computeSignature builds the colon-delimited hashing payload from the event and
// returns Base64(HMAC-SHA256(payload, secret)). This is the single source of
// truth for the signing scheme — the verifier and tests both use it.
func computeSignature(ev *WebhookEvent, timestamp, secret string) string {
	payload := strings.Join([]string{
		ev.EventType,
		ev.RequestID,
		ev.Data.Merchant.UserID,
		ev.Data.Merchant.WalletID,
		ev.TxID(),
		ev.Data.Transaction.Type,
		ev.Data.Transaction.Time,
		ev.Data.Transaction.ResponseCode,
		timestamp,
	}, ":")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
