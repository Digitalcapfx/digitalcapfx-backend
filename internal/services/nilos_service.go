package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/nilos"
	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

// ErrNilosWebhookSignature is returned when a Nilos webhook fails signature
// verification; the handler maps it to 401.
var ErrNilosWebhookSignature = errors.New("invalid nilos webhook signature")

// NilosService owns the Nilos fiat rails balance concerns: crediting inbound
// deposits to the ledger (idempotently) and reading the authoritative live
// balance for display/verification. Payouts/quotes live in the withdrawal and
// exchange services.
type NilosService struct {
	pool          *pgxpool.Pool
	client        *nilos.Client
	notif         *NotificationService
	webhookSecret string
	logger        *zap.Logger
}

func NewNilosService(pool *pgxpool.Pool, client *nilos.Client, notif *NotificationService, webhookSecret string, logger *zap.Logger) *NilosService {
	return &NilosService{pool: pool, client: client, notif: notif, webhookSecret: webhookSecret, logger: logger}
}

// GetLiveBalance returns the authoritative Nilos balance for a Nilos-backed
// account (EUR/GBP/…). This is the money actually held at Nilos — use it to
// display and to reconcile against the ledger. Returns ok=false when the account
// is not Nilos-backed or the read fails.
func (s *NilosService) GetLiveBalance(ctx context.Context, acc db.Account) (float64, bool) {
	if s.client == nil || acc.NilosAccountID == nil || *acc.NilosAccountID == "" {
		return 0, false
	}
	a, err := s.client.GetAccount(ctx, *acc.NilosAccountID)
	if err != nil {
		s.logger.Warn("nilos live balance fetch failed",
			zap.String("nilos_id", *acc.NilosAccountID), zap.Error(err))
		return 0, false
	}
	return a.BalanceFor(acc.Currency), true
}

// HandleWebhook verifies and processes a Nilos webhook. Inbound, successful
// transactions credit the owning account's ledger idempotently (keyed on the
// Nilos transaction id). NOTE: the Nilos webhook payload shape and signing
// scheme are not fully documented — the parser and verifier are permissive and
// MUST be confirmed against the first real delivery (mismatches are logged).
func (s *NilosService) HandleWebhook(ctx context.Context, body []byte, headers http.Header) error {
	if s.webhookSecret != "" {
		if !verifyNilosSignature(headers, body, []byte(s.webhookSecret)) {
			s.logger.Warn("nilos webhook signature verification failed",
				zap.Strings("x_headers", nilosSignatureHeaders(headers)))
			return ErrNilosWebhookSignature
		}
	}

	tx, ok := parseNilosDeposit(body)
	if !ok {
		s.logger.Info("nilos webhook: not a creditable inbound deposit — ignored")
		return nil
	}

	q := db.New(s.pool)
	acc, err := q.GetAccountByNilosAccountID(ctx, &tx.AccountID)
	if err != nil {
		s.logger.Warn("nilos webhook: no account for nilos id", zap.String("nilos_id", tx.AccountID))
		return nil
	}

	credited, err := creditDepositOnce(ctx, s.pool, "nilos", tx.eventID(), acc.ID, tx.Currency, tx.Amount)
	if err != nil {
		return fmt.Errorf("credit nilos deposit: %w", err)
	}
	if !credited {
		s.logger.Info("nilos webhook: duplicate event ignored", zap.String("event_id", tx.eventID()))
		return nil
	}

	s.logger.Info("nilos deposit credited",
		zap.String("user_id", acc.UserID.String()),
		zap.String("currency", tx.Currency),
		zap.Float64("amount", tx.Amount))

	s.notif.Create(ctx, CreateNotificationInput{
		UserID: acc.UserID,
		Type:   NotifDepositConfirmed,
		Title:  fmt.Sprintf("Received %.2f %s", tx.Amount, tx.Currency),
		Body:   fmt.Sprintf("Your %s account was credited with %.2f %s.", tx.Currency, tx.Amount, tx.Currency),
		Metadata: map[string]string{
			"currency":  tx.Currency,
			"amount":    fmt.Sprintf("%.2f", tx.Amount),
			"reference": tx.Reference,
			"source":    "nilos",
		},
	})
	return nil
}

// ─── Webhook payload parsing (permissive) ─────────────────────────────────────

type rawNilosTx struct {
	ID            string  `json:"id"`
	TransactionID string  `json:"transactionId"`
	Reference     string  `json:"reference"`
	ExternalID    string  `json:"externalId"`
	Currency      string  `json:"currency"`
	AccountID     string  `json:"accountId"`
	Amount        float64 `json:"amount"`
	IsInbound     *bool   `json:"isInbound"`
	Status        string  `json:"status"`
}

func (t rawNilosTx) eventID() string {
	switch {
	case t.ID != "":
		return t.ID
	case t.TransactionID != "":
		return t.TransactionID
	case t.ExternalID != "":
		return t.ExternalID
	default:
		return t.Reference
	}
}

// parseNilosDeposit extracts a creditable inbound deposit from the webhook body,
// trying { data: { transaction } }, { data }, and top-level shapes. Only an
// explicitly inbound, successful, positive-amount transaction with an accountId
// is returned.
func parseNilosDeposit(body []byte) (rawNilosTx, bool) {
	var tx *rawNilosTx

	var nested struct {
		Data struct {
			Transaction *rawNilosTx `json:"transaction"`
		} `json:"data"`
	}
	var dataOnly struct {
		Data rawNilosTx `json:"data"`
	}
	var top rawNilosTx

	if json.Unmarshal(body, &nested) == nil && nested.Data.Transaction != nil && nested.Data.Transaction.AccountID != "" {
		tx = nested.Data.Transaction
	} else if json.Unmarshal(body, &dataOnly) == nil && dataOnly.Data.AccountID != "" {
		tx = &dataOnly.Data
	} else if json.Unmarshal(body, &top) == nil && top.AccountID != "" {
		tx = &top
	}
	if tx == nil {
		return rawNilosTx{}, false
	}

	if tx.IsInbound == nil || !*tx.IsInbound {
		return rawNilosTx{}, false
	}
	if tx.Amount <= 0 || tx.Currency == "" {
		return rawNilosTx{}, false
	}
	st := strings.ToLower(tx.Status)
	if !(strings.Contains(st, "success") || st == "completed" || st == "settled") {
		return rawNilosTx{}, false
	}
	return *tx, true
}

// ─── Signature verification (permissive) ──────────────────────────────────────

var nilosSigHeaderNames = []string{
	"X-Api-Signature", "X-Nilos-Signature", "X-Signature",
	"X-Webhook-Signature", "Webhook-Signature",
}

func verifyNilosSignature(headers http.Header, body, secret []byte) bool {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sum := mac.Sum(nil)
	expHex := hex.EncodeToString(sum)
	expB64 := base64.StdEncoding.EncodeToString(sum)

	for _, name := range nilosSigHeaderNames {
		got := strings.TrimSpace(headers.Get(name))
		if got == "" {
			continue
		}
		got = strings.TrimPrefix(got, "sha256=")
		if hmac.Equal([]byte(got), []byte(expHex)) || hmac.Equal([]byte(got), []byte(expB64)) {
			return true
		}
	}
	return false
}

func nilosSignatureHeaders(r http.Header) []string {
	var found []string
	for name := range r {
		l := strings.ToLower(name)
		if strings.HasPrefix(l, "x-") || strings.Contains(l, "signature") {
			found = append(found, name)
		}
	}
	return found
}
