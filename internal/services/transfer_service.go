package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
	"github.com/rachfinance/digitalfx/internal/pkg/email"
)

var (
	// ErrTransferToSelf is returned when the sender and receiver resolve to the
	// same DigitalFX user.
	ErrTransferToSelf = errors.New("cannot transfer to yourself")
	// ErrTransferReceiverNotFound is returned when no user matches the phone.
	ErrTransferReceiverNotFound = errors.New("no DigitalFX user found with that phone number")
	// ErrTransferInvalidAmount is returned for a non-positive amount.
	ErrTransferInvalidAmount = errors.New("amount must be greater than zero")
)

// TransferService moves fiat between two DigitalFX users' internal ledger
// accounts. For XAF/XOF this is an instant, free, peer-to-peer rail: it only
// debits/credits the internal `accounts` ledger and never touches an external
// provider (no Hub2, no Nilos, no on-chain leg).
type TransferService struct {
	pool        *pgxpool.Pool
	notif       *NotificationService
	emailClient *email.Client
	appBaseURL  string
	logger      *zap.Logger
}

func NewTransferService(pool *pgxpool.Pool, notif *NotificationService, emailClient *email.Client, appBaseURL string, logger *zap.Logger) *TransferService {
	return &TransferService{pool: pool, notif: notif, emailClient: emailClient, appBaseURL: appBaseURL, logger: logger}
}

type InternalTransferInput struct {
	SenderID      uuid.UUID
	ReceiverPhone string
	Currency      string
	Amount        float64
	Description   string
}

type InternalTransferResult struct {
	Reference     string    `json:"reference"`
	Currency      string    `json:"currency"`
	Amount        float64   `json:"amount"`
	Fee           float64   `json:"fee"`
	SenderTxID    uuid.UUID `json:"sender_transaction_id"`
	ReceiverTxID  uuid.UUID `json:"receiver_transaction_id"`
	ReceiverName  string    `json:"receiver_name"`
	ReceiverPhone string    `json:"receiver_phone"`
	Status        string    `json:"status"`
}

// InternalTransfer atomically debits the sender and credits the receiver of the
// same currency, recording a transaction leg for each side, then notifies both
// parties. XAF/XOF transfers are instant and free — there is no fee and no
// external settlement, so on success the money has already moved.
func (s *TransferService) InternalTransfer(ctx context.Context, in InternalTransferInput) (*InternalTransferResult, error) {
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		return nil, ErrUnsupportedCurrency
	}
	// Instant, free, internal-ledger p2p is only offered for the mobile-money
	// currencies (XAF/XOF); other currencies settle over their own rail.
	if !IsMobileMoneyCurrency(in.Currency) {
		return nil, ErrUnsupportedCurrency
	}
	if in.Amount <= 0 {
		return nil, ErrTransferInvalidAmount
	}

	q := db.New(s.pool)

	// Resolve the receiver by phone (accepts any format) and reject self-send.
	receiver, err := q.GetUserByPhoneAny(ctx, phoneCandidates(in.ReceiverPhone))
	if err != nil {
		return nil, ErrTransferReceiverNotFound
	}
	if receiver.ID == in.SenderID {
		return nil, ErrTransferToSelf
	}

	// Both parties must already hold an account in the requested currency.
	senderAcc, err := q.GetAccountByUserAndCurrency(ctx, db.GetAccountByUserAndCurrencyParams{UserID: in.SenderID, Currency: in.Currency})
	if err != nil {
		return nil, ErrAccountNotFound
	}
	receiverAcc, err := q.GetAccountByUserAndCurrency(ctx, db.GetAccountByUserAndCurrencyParams{UserID: receiver.ID, Currency: in.Currency})
	if err != nil {
		return nil, ErrAccountNotFound
	}

	amt, err := numericFromFloat(in.Amount)
	if err != nil {
		return nil, fmt.Errorf("encode amount: %w", err)
	}
	amountS := amtStr(in.Amount)
	const feeS = "0.00"
	reference := "TRF-" + uuid.NewString()

	sender, _ := q.GetUserByID(ctx, in.SenderID)
	senderName := userDisplayName(sender.FirstName, sender.LastName, sender.PhoneNumber)
	receiverName := userDisplayName(receiver.FirstName, receiver.LastName, receiver.PhoneNumber)

	desc := strings.TrimSpace(in.Description)
	if desc == "" {
		desc = "Transfer"
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transfer tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(tx)

	// Debit the sender first. DebitAccount only touches a row when
	// available_balance >= amount, so pgx.ErrNoRows means insufficient funds.
	if _, err := qtx.DebitAccount(ctx, db.DebitAccountParams{ID: senderAcc.ID, Balance: amt}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInsufficientFunds
		}
		return nil, fmt.Errorf("debit sender: %w", err)
	}
	if _, err := qtx.CreditAccount(ctx, db.CreditAccountParams{ID: receiverAcc.ID, Balance: amt}); err != nil {
		return nil, fmt.Errorf("credit receiver: %w", err)
	}

	baseMeta := map[string]any{
		"transfer_reference": reference,
		"sender_id":          in.SenderID.String(),
		"receiver_id":        receiver.ID.String(),
		"sender_phone":       sender.PhoneNumber,
		"receiver_phone":     receiver.PhoneNumber,
	}
	senderTxID, err := createFiatLeg(ctx, qtx, senderAcc.ID, reference+"-OUT", "transfer_out",
		amountS, in.Currency, feeS, "Transfer to "+receiverName+" — "+desc, "completed", baseMeta)
	if err != nil {
		return nil, fmt.Errorf("record sender transaction: %w", err)
	}
	receiverTxID, err := createFiatLeg(ctx, qtx, receiverAcc.ID, reference+"-IN", "transfer_in",
		amountS, in.Currency, feeS, "Transfer from "+senderName+" — "+desc, "completed", baseMeta)
	if err != nil {
		return nil, fmt.Errorf("record receiver transaction: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transfer tx: %w", err)
	}

	// Money has moved. Notifications and email are best-effort — a failure here
	// must never surface as a failed transfer.
	s.notif.Create(ctx, CreateNotificationInput{
		UserID: in.SenderID,
		Type:   NotifTransferSent,
		Title:  fmt.Sprintf("Sent %s %s", amountS, in.Currency),
		Body:   fmt.Sprintf("You sent %s %s to %s.", amountS, in.Currency, receiverName),
		Metadata: map[string]string{
			"reference": reference, "amount": amountS, "currency": in.Currency,
			"receiver": receiverName, "receiver_phone": receiver.PhoneNumber,
		},
	})
	s.notif.Create(ctx, CreateNotificationInput{
		UserID: receiver.ID,
		Type:   NotifTransferRecvd,
		Title:  fmt.Sprintf("Received %s %s", amountS, in.Currency),
		Body:   fmt.Sprintf("%s sent you %s %s.", senderName, amountS, in.Currency),
		Metadata: map[string]string{
			"reference": reference, "amount": amountS, "currency": in.Currency,
			"sender": senderName, "sender_phone": sender.PhoneNumber,
		},
	})
	s.emailIncomingTransfer(receiver.Email, receiverName, senderName, amountS, in.Currency, desc)

	return &InternalTransferResult{
		Reference:     reference,
		Currency:      in.Currency,
		Amount:        in.Amount,
		Fee:           0,
		SenderTxID:    senderTxID,
		ReceiverTxID:  receiverTxID,
		ReceiverName:  receiverName,
		ReceiverPhone: receiver.PhoneNumber,
		Status:        "completed",
	}, nil
}

// emailIncomingTransfer notifies the receiver by email that funds have landed.
// Fire-and-forget: the SMTP Send is synchronous, so run it off the request path.
func (s *TransferService) emailIncomingTransfer(to *string, receiverName, senderName, amount, currency, note string) {
	if s.emailClient == nil || to == nil || *to == "" {
		return
	}
	subject := fmt.Sprintf("You received %s %s", amount, currency)
	body := fmt.Sprintf(`<div style="font-family:Arial,sans-serif;max-width:520px;margin:0 auto">
  <h2 style="color:#111">Money received</h2>
  <p>Hi %s,</p>
  <p><strong>%s</strong> just sent you <strong>%s %s</strong>.</p>
  <p style="color:#555">Note: %s</p>
  <p>The funds are already available in your %s balance.</p>
  <p style="margin-top:24px"><a href="%s" style="background:#111;color:#fff;padding:10px 18px;border-radius:6px;text-decoration:none">Open DigitalFX</a></p>
</div>`, receiverName, senderName, amount, currency, note, currency, s.appBaseURL)
	addr := *to
	go func() {
		if err := s.emailClient.Send(addr, subject, body); err != nil {
			s.logger.Warn("transfer receiver email failed", zap.Error(err))
		}
	}()
}

// userDisplayName renders a human-friendly name, falling back to the phone
// number when the profile has no name set.
func userDisplayName(first, last, phone string) string {
	name := strings.TrimSpace(strings.TrimSpace(first) + " " + strings.TrimSpace(last))
	if name == "" {
		return phone
	}
	return name
}

// createFiatLeg inserts a single ledger transaction leg and returns its
// (DB-generated) id so the caller can surface it to the API response.
func createFiatLeg(ctx context.Context, q *db.Queries, accountID uuid.UUID, ref, txType, amount, currency, fee, description, status string, meta map[string]any) (uuid.UUID, error) {
	raw, _ := json.Marshal(meta)
	desc := description
	tx, err := q.CreateFiatTransaction(ctx, db.CreateFiatTransactionParams{
		AccountID: accountID, Reference: ref, Type: txType,
		Amount: amount, Currency: currency, Fee: fee, Description: &desc, Status: status, Metadata: raw,
	})
	return tx.ID, err
}
