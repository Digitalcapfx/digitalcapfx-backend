package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/nomba"
	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

// ErrNombaWebhookSignature is returned when an inbound Nomba webhook fails
// signature verification; the HTTP handler maps it to 401.
var ErrNombaWebhookSignature = errors.New("invalid nomba webhook signature")

// NombaService owns the Nigerian NGN rails backed by Nomba: inbound credits to
// virtual accounts (via webhook), bank-code + name lookups, and NGN payouts.
type NombaService struct {
	pool          *pgxpool.Pool
	client        *nomba.Client
	notif         *NotificationService
	webhookSecret string
	logger        *zap.Logger
}

func NewNombaService(pool *pgxpool.Pool, client *nomba.Client, notif *NotificationService, webhookSecret string, logger *zap.Logger) *NombaService {
	return &NombaService{pool: pool, client: client, notif: notif, webhookSecret: webhookSecret, logger: logger}
}

// ListBanks returns the supported Nigerian bank codes + names (for building
// payout recipients / the bank picker).
func (s *NombaService) ListBanks(ctx context.Context) ([]nomba.Bank, error) {
	if s.client == nil || !s.client.Configured() {
		return nil, errors.New("nomba not configured")
	}
	return s.client.ListBanks(ctx)
}

// LookupAccount resolves the account holder name for a Nigerian bank account so
// the sender can confirm the recipient before transferring.
func (s *NombaService) LookupAccount(ctx context.Context, accountNumber, bankCode string) (*nomba.AccountLookupResult, error) {
	if s.client == nil || !s.client.Configured() {
		return nil, errors.New("nomba not configured")
	}
	return s.client.LookupBankAccount(ctx, accountNumber, bankCode)
}

// HandleWebhook verifies and processes an inbound Nomba webhook. When a webhook
// secret is configured it verifies the signature and returns
// ErrNombaWebhookSignature on mismatch (the handler maps that to 401). It then
// dispatches on the event type.
func (s *NombaService) HandleWebhook(ctx context.Context, body []byte, headers http.Header) error {
	ev, err := nomba.ParseWebhook(body)
	if err != nil {
		return fmt.Errorf("parse nomba webhook: %w", err)
	}

	if s.webhookSecret != "" {
		if !nomba.VerifyWebhookSignature(headers, ev, s.webhookSecret) {
			s.logger.Warn("nomba webhook signature verification failed",
				zap.String("event", ev.EventType),
				zap.String("request_id", ev.RequestID))
			return ErrNombaWebhookSignature
		}
	}

	s.logger.Info("nomba webhook received",
		zap.String("event", ev.EventType),
		zap.String("request_id", ev.RequestID),
		zap.String("alias_account", ev.Data.Transaction.AliasAccountNumber),
		zap.Float64("amount", ev.Data.Transaction.TransactionAmount))

	switch ev.EventType {
	case nomba.EventPaymentSuccess:
		return s.creditVirtualAccount(ctx, ev)
	case nomba.EventPayoutSuccess, nomba.EventPayoutFailed, nomba.EventPayoutRefund,
		nomba.EventPaymentFailed, nomba.EventPaymentReversal:
		// Payout reconciliation and failed/reversed payments are logged for now;
		// balance movements for payouts are applied at request time.
		s.logger.Info("nomba webhook: no ledger action for event", zap.String("event", ev.EventType))
		return nil
	default:
		s.logger.Warn("nomba webhook: unknown event type", zap.String("event", ev.EventType))
		return nil
	}
}

// creditVirtualAccount credits an inbound NGN transfer to the owning account,
// resolved by the virtual (alias) account number the money was sent to.
func (s *NombaService) creditVirtualAccount(ctx context.Context, ev *nomba.WebhookEvent) error {
	alias := ev.Data.Transaction.AliasAccountNumber
	if alias == "" {
		s.logger.Warn("nomba credit webhook missing alias account number", zap.String("request_id", ev.RequestID))
		return nil
	}
	amount := ev.Data.Transaction.TransactionAmount
	if amount <= 0 {
		s.logger.Warn("nomba credit webhook non-positive amount", zap.String("alias", alias))
		return nil
	}

	q := db.New(s.pool)
	account, err := q.GetAccountByNombaAccountNumber(ctx, &alias)
	if err != nil {
		// Unknown account — do not error (avoid provider retries); just log.
		s.logger.Warn("nomba credit webhook: no account for alias", zap.String("alias", alias))
		return nil
	}

	var amt pgtype.Numeric
	if err := amt.Scan(fmt.Sprintf("%.6f", amount)); err != nil {
		return fmt.Errorf("encode nomba credit amount: %w", err)
	}
	if _, err := q.CreditAccount(ctx, db.CreditAccountParams{ID: account.ID, Balance: amt}); err != nil {
		return fmt.Errorf("credit NGN account %s: %w", account.ID, err)
	}

	s.logger.Info("nomba NGN credit applied",
		zap.String("user_id", account.UserID.String()),
		zap.String("alias", alias),
		zap.Float64("amount", amount))

	s.notif.Create(ctx, CreateNotificationInput{
		UserID: account.UserID,
		Type:   NotifDepositConfirmed,
		Title:  fmt.Sprintf("Received ₦%.2f", amount),
		Body:   fmt.Sprintf("Your Naira account was credited with ₦%.2f from %s.", amount, ev.Data.Customer.SenderName),
		Metadata: map[string]string{
			"currency":  "NGN",
			"amount":    fmt.Sprintf("%.2f", amount),
			"reference": ev.TxID(),
			"source":    "nomba",
		},
	})
	return nil
}
