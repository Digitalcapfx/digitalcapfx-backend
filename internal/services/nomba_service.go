package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

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

// GetWalletBalance returns the aggregate Nomba merchant (parent) NGN balance,
// for treasury reconciliation against the sum of NGN ledger balances. This is
// NOT a per-customer balance — Nomba virtual accounts are collection aliases.
func (s *NombaService) GetWalletBalance(ctx context.Context) (float64, error) {
	if s.client == nil || !s.client.Configured() {
		return 0, errors.New("nomba not configured")
	}
	bal, err := s.client.GetWalletBalance(ctx)
	if err != nil {
		return 0, err
	}
	f, _ := strconv.ParseFloat(strings.TrimSpace(bal.Amount), 64)
	return f, nil
}

// NombaReconciliation compares the Nomba merchant wallet balance against the sum
// of all customers' NGN ledger balances. A non-zero difference flags drift
// between what Nomba holds and what our ledger says we owe customers.
type NombaReconciliation struct {
	Currency           string  `json:"currency"`
	NombaWalletBalance float64 `json:"nomba_wallet_balance"`
	LedgerTotalNGN     float64 `json:"ledger_total_ngn"`
	Difference         float64 `json:"difference"`
}

// Reconcile returns the NGN treasury reconciliation (Nomba wallet vs ledger).
func (s *NombaService) Reconcile(ctx context.Context) (*NombaReconciliation, error) {
	wallet, err := s.GetWalletBalance(ctx)
	if err != nil {
		return nil, err
	}
	q := db.New(s.pool)
	sum, _ := q.SumAccountBalanceByCurrency(ctx, "NGN")
	ledger := pgNumericToFloat(sum)
	return &NombaReconciliation{
		Currency:           "NGN",
		NombaWalletBalance: wallet,
		LedgerTotalNGN:     ledger,
		Difference:         wallet - ledger,
	}, nil
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

	// Idempotent credit: keyed on the Nomba transaction id so a redelivered
	// webhook never inflates the balance.
	credited, err := creditDepositOnce(ctx, s.pool, "nomba", ev.TxID(), account.ID, "NGN", amount)
	if err != nil {
		return fmt.Errorf("credit NGN account %s: %w", account.ID, err)
	}
	if !credited {
		s.logger.Info("nomba credit webhook: duplicate event ignored", zap.String("tx_id", ev.TxID()))
		return nil
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
