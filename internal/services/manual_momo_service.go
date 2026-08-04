package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
	"github.com/rachfinance/digitalfx/internal/pkg/email"
)

// Manual mobile-money errors.
var (
	ErrMomoNotFound            = errors.New("mobile money number not found")
	ErrMomoInactive            = errors.New("mobile money number is not active")
	ErrManualNotFound          = errors.New("request not found")
	ErrManualAlreadyReviewed   = errors.New("request has already been reviewed")
	ErrMomoInvalidAmount       = errors.New("amount must be greater than zero")
	ErrMomoUnsupportedCurrency = errors.New("manual mobile money supports XOF and XAF only")
	ErrMomoInsufficientFunds   = errors.New("insufficient balance")
	ErrMomoCreditTooHigh       = errors.New("credited amount cannot exceed the claimed amount")
	ErrMomoChargeTooHigh       = errors.New("charge cannot exceed the withdrawal amount")
)

// ManualMomoService runs the manual mobile-money rails: the business publishes
// its own collection numbers, customers claim payments they made out-of-band,
// and admins confirm/reject manually — crediting or debiting the ledger after
// taking a charge. It runs alongside (never replaces) the automated Hub2 rail.
type ManualMomoService struct {
	pool        *pgxpool.Pool
	notif       *NotificationService
	emailClient *email.Client
	adminEmail  string
	appBaseURL  string
	logger      *zap.Logger
}

func NewManualMomoService(pool *pgxpool.Pool, notif *NotificationService, emailClient *email.Client, adminEmail, appBaseURL string, logger *zap.Logger) *ManualMomoService {
	return &ManualMomoService{pool: pool, notif: notif, emailClient: emailClient, adminEmail: adminEmail, appBaseURL: appBaseURL, logger: logger}
}

func momoSupportedCurrency(c string) bool { return c == "XOF" || c == "XAF" }

func amtStr(f float64) string { return fmt.Sprintf("%.2f", f) }

func numericFromFloat(f float64) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	err := n.Scan(fmt.Sprintf("%.6f", f))
	return n, err
}

// ─── Business collection numbers ─────────────────────────────────────────────

// ListActiveMomoAccounts returns the active collection numbers a customer can pay to.
func (s *ManualMomoService) ListActiveMomoAccounts(ctx context.Context) ([]db.ManualMomoAccount, error) {
	return db.New(s.pool).ListActiveMomoAccounts(ctx)
}

func (s *ManualMomoService) ListAllMomoAccounts(ctx context.Context) ([]db.ManualMomoAccount, error) {
	return db.New(s.pool).ListAllMomoAccounts(ctx)
}

type MomoAccountInput struct {
	Provider     string
	DisplayName  string
	PhoneNumber  string
	AccountName  string
	Currency     string
	Country      string
	Instructions string
	IsActive     bool
	SortOrder    int32
}

func (s *ManualMomoService) CreateMomoAccount(ctx context.Context, in MomoAccountInput) (*db.ManualMomoAccount, error) {
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = "XOF"
	}
	if !momoSupportedCurrency(in.Currency) {
		return nil, ErrMomoUnsupportedCurrency
	}
	if in.Country == "" {
		in.Country = "CI"
	}
	acc, err := db.New(s.pool).CreateMomoAccount(ctx, db.CreateMomoAccountParams{
		Provider:     in.Provider,
		DisplayName:  in.DisplayName,
		PhoneNumber:  in.PhoneNumber,
		AccountName:  strPtrOrNil(in.AccountName),
		Currency:     in.Currency,
		Country:      in.Country,
		Instructions: strPtrOrNil(in.Instructions),
		IsActive:     in.IsActive,
		SortOrder:    in.SortOrder,
	})
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

func (s *ManualMomoService) UpdateMomoAccount(ctx context.Context, id uuid.UUID, in MomoAccountInput) (*db.ManualMomoAccount, error) {
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if !momoSupportedCurrency(in.Currency) {
		return nil, ErrMomoUnsupportedCurrency
	}
	q := db.New(s.pool)
	if _, err := q.GetMomoAccount(ctx, id); err != nil {
		return nil, ErrMomoNotFound
	}
	acc, err := q.UpdateMomoAccount(ctx, db.UpdateMomoAccountParams{
		ID:           id,
		Provider:     in.Provider,
		DisplayName:  in.DisplayName,
		PhoneNumber:  in.PhoneNumber,
		AccountName:  strPtrOrNil(in.AccountName),
		Currency:     in.Currency,
		Country:      in.Country,
		Instructions: strPtrOrNil(in.Instructions),
		IsActive:     in.IsActive,
		SortOrder:    in.SortOrder,
	})
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

func (s *ManualMomoService) DeleteMomoAccount(ctx context.Context, id uuid.UUID) error {
	return db.New(s.pool).DeleteMomoAccount(ctx, id)
}

// ─── Deposits (customer claims a payment made to a business number) ──────────

type SubmitDepositInput struct {
	UserID        uuid.UUID
	MomoAccountID uuid.UUID
	Amount        float64
	SenderPhone   string
	SenderName    string
	Reference     string
	Note          string
}

func (s *ManualMomoService) SubmitDeposit(ctx context.Context, in SubmitDepositInput) (*db.ManualDeposit, error) {
	if in.Amount <= 0 {
		return nil, ErrMomoInvalidAmount
	}
	q := db.New(s.pool)

	momo, err := q.GetMomoAccount(ctx, in.MomoAccountID)
	if err != nil {
		return nil, ErrMomoNotFound
	}
	if !momo.IsActive {
		return nil, ErrMomoInactive
	}

	// The deposit credits the customer's account in the number's currency.
	acc, err := q.GetAccountByUserAndCurrency(ctx, db.GetAccountByUserAndCurrencyParams{
		UserID:   in.UserID,
		Currency: momo.Currency,
	})
	if err != nil {
		return nil, ErrAccountNotFound
	}

	momoID := momo.ID
	dep, err := q.CreateManualDeposit(ctx, db.CreateManualDepositParams{
		UserID:        in.UserID,
		AccountID:     acc.ID,
		MomoAccountID: &momoID,
		Provider:      momo.Provider,
		Currency:      momo.Currency,
		ClaimedAmount: amtStr(in.Amount),
		SenderPhone:   strPtrOrNil(in.SenderPhone),
		SenderName:    strPtrOrNil(in.SenderName),
		Reference:     strPtrOrNil(in.Reference),
		Note:          strPtrOrNil(in.Note),
	})
	if err != nil {
		return nil, fmt.Errorf("create manual deposit: %w", err)
	}

	// Notify the customer their claim is pending, and alert admins to confirm.
	s.notif.Create(ctx, CreateNotificationInput{
		UserID: in.UserID,
		Type:   NotifManualDepositSubmitted,
		Title:  "Deposit received — pending confirmation",
		Body:   fmt.Sprintf("We've logged your %s %s payment via %s. It will reflect once our team confirms receipt.", amtStr(in.Amount), momo.Currency, momo.DisplayName),
		Metadata: map[string]string{"deposit_id": dep.ID.String(), "provider": momo.Provider, "amount": amtStr(in.Amount), "currency": momo.Currency},
	})
	go s.alertAdmins(ctx, "deposit", in.UserID, momo.DisplayName, momo.Currency, amtStr(in.Amount), in.SenderPhone, in.Reference, in.Note)

	return &dep, nil
}

func (s *ManualMomoService) ListUserDeposits(ctx context.Context, userID uuid.UUID, page, perPage int32) ([]db.ManualDeposit, error) {
	limit, offset := pageBounds(page, perPage)
	return db.New(s.pool).ListManualDepositsByUser(ctx, db.ListManualDepositsByUserParams{UserID: userID, Limit: limit, Offset: offset})
}

func (s *ManualMomoService) ListDepositsByStatus(ctx context.Context, status string, page, perPage int32) ([]db.ManualDeposit, error) {
	limit, offset := pageBounds(page, perPage)
	return db.New(s.pool).ListManualDepositsByStatus(ctx, db.ListManualDepositsByStatusParams{Status: status, Lim: limit, Off: offset})
}

// ConfirmDeposit credits the customer's ledger with creditedAmount (the amount
// after the business's charge) and marks the claim confirmed — atomically and
// idempotently (a second confirm finds no pending row and is a no-op error).
func (s *ManualMomoService) ConfirmDeposit(ctx context.Context, depositID, staffID uuid.UUID, creditedAmount float64, adminNote string) (*db.ManualDeposit, error) {
	if creditedAmount <= 0 {
		return nil, ErrMomoInvalidAmount
	}
	q := db.New(s.pool)
	existing, err := q.GetManualDeposit(ctx, depositID)
	if err != nil {
		return nil, ErrManualNotFound
	}
	if existing.Status != "pending" {
		return nil, ErrManualAlreadyReviewed
	}
	claimed := parseFloatSafe(existing.ClaimedAmount)
	if creditedAmount > claimed+0.0001 {
		return nil, ErrMomoCreditTooHigh
	}
	charge := claimed - creditedAmount
	if charge < 0 {
		charge = 0
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin confirm tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	qtx := db.New(tx)
	creditedS, chargeS, note := amtStr(creditedAmount), amtStr(charge), strPtrOrNil(adminNote)
	dep, err := qtx.ConfirmManualDeposit(ctx, db.ConfirmManualDepositParams{
		ID:             depositID,
		CreditedAmount: &creditedS,
		Charge:         &chargeS,
		AdminNote:      note,
		ReviewedBy:     staffPtr(staffID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrManualAlreadyReviewed
	}
	if err != nil {
		return nil, fmt.Errorf("confirm manual deposit: %w", err)
	}
	amt, err := numericFromFloat(creditedAmount)
	if err != nil {
		return nil, fmt.Errorf("encode amount: %w", err)
	}
	if _, err := qtx.CreditAccount(ctx, db.CreditAccountParams{ID: dep.AccountID, Balance: amt}); err != nil {
		return nil, fmt.Errorf("credit account: %w", err)
	}
	// Mirror the credit into the canonical transactions table (customer history).
	if err := recordFiatTx(ctx, qtx, dep.AccountID, "MMD-"+dep.ID.String(), "deposit",
		creditedS, dep.Currency, chargeS, "Mobile money deposit ("+dep.Provider+")", "completed",
		map[string]any{"source": "manual_momo", "deposit_id": dep.ID.String(), "provider": dep.Provider}); err != nil {
		return nil, fmt.Errorf("record deposit transaction: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit confirm tx: %w", err)
	}

	s.notif.Create(ctx, CreateNotificationInput{
		UserID: dep.UserID,
		Type:   NotifManualDepositConfirmed,
		Title:  fmt.Sprintf("Deposit confirmed — %s %s", creditedS, dep.Currency),
		Body:   fmt.Sprintf("Your %s account has been credited with %s %s.", dep.Currency, creditedS, dep.Currency),
		Metadata: map[string]string{"deposit_id": dep.ID.String(), "credited": creditedS, "charge": chargeS, "currency": dep.Currency},
	})
	return &dep, nil
}

func (s *ManualMomoService) RejectDeposit(ctx context.Context, depositID, staffID uuid.UUID, adminNote string) (*db.ManualDeposit, error) {
	q := db.New(s.pool)
	note := strPtrOrNil(adminNote)
	dep, err := q.RejectManualDeposit(ctx, db.RejectManualDepositParams{ID: depositID, AdminNote: note, ReviewedBy: staffPtr(staffID)})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, gerr := q.GetManualDeposit(ctx, depositID); gerr != nil {
			return nil, ErrManualNotFound
		}
		return nil, ErrManualAlreadyReviewed
	}
	if err != nil {
		return nil, fmt.Errorf("reject manual deposit: %w", err)
	}
	s.notif.Create(ctx, CreateNotificationInput{
		UserID: dep.UserID,
		Type:   NotifManualDepositRejected,
		Title:  "Deposit could not be confirmed",
		Body:   "We couldn't confirm your recent mobile-money payment. Please contact support with your payment reference.",
		Metadata: map[string]string{"deposit_id": dep.ID.String()},
	})
	return &dep, nil
}

// ─── Withdrawals (customer cash-out to a mobile-money number) ─────────────────

type RequestWithdrawalInput struct {
	UserID         uuid.UUID
	Currency       string
	Provider       string
	Amount         float64
	RecipientPhone string
	RecipientName  string
	Note           string
}

// RequestWithdrawal validates and immediately debits (holds) the amount from the
// customer's balance, then records a pending cash-out for an admin to pay out
// manually. Holding up-front prevents double-spend; a rejection refunds it.
func (s *ManualMomoService) RequestWithdrawal(ctx context.Context, in RequestWithdrawalInput) (*db.ManualWithdrawal, error) {
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if !momoSupportedCurrency(in.Currency) {
		return nil, ErrMomoUnsupportedCurrency
	}
	if in.Amount <= 0 {
		return nil, ErrMomoInvalidAmount
	}
	q := db.New(s.pool)
	acc, err := q.GetAccountByUserAndCurrency(ctx, db.GetAccountByUserAndCurrencyParams{UserID: in.UserID, Currency: in.Currency})
	if err != nil {
		return nil, ErrAccountNotFound
	}

	amt, err := numericFromFloat(in.Amount)
	if err != nil {
		return nil, fmt.Errorf("encode amount: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin withdrawal tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(tx)

	// DebitAccount only succeeds when available_balance >= amount (else no row).
	if _, err := qtx.DebitAccount(ctx, db.DebitAccountParams{ID: acc.ID, Balance: amt}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMomoInsufficientFunds
		}
		return nil, fmt.Errorf("hold funds: %w", err)
	}
	w, err := qtx.CreateManualWithdrawal(ctx, db.CreateManualWithdrawalParams{
		UserID:         in.UserID,
		AccountID:      acc.ID,
		Provider:       in.Provider,
		Currency:       in.Currency,
		Amount:         amtStr(in.Amount),
		RecipientPhone: in.RecipientPhone,
		RecipientName:  strPtrOrNil(in.RecipientName),
		Note:           strPtrOrNil(in.Note),
	})
	if err != nil {
		return nil, fmt.Errorf("create manual withdrawal: %w", err)
	}
	// Mirror the held debit into the transactions table as pending (customer history).
	if err := recordFiatTx(ctx, qtx, acc.ID, "MMW-"+w.ID.String(), "withdrawal",
		amtStr(in.Amount), in.Currency, "0.00", "Mobile money cash-out to "+in.RecipientPhone, "pending",
		map[string]any{"source": "manual_momo", "withdrawal_id": w.ID.String(), "provider": in.Provider}); err != nil {
		return nil, fmt.Errorf("record withdrawal transaction: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit withdrawal tx: %w", err)
	}

	s.notif.Create(ctx, CreateNotificationInput{
		UserID: in.UserID,
		Type:   NotifManualWithdrawalRequested,
		Title:  "Cash-out request received",
		Body:   fmt.Sprintf("Your request to send %s %s to %s is pending. We'll process it shortly.", amtStr(in.Amount), in.Currency, in.RecipientPhone),
		Metadata: map[string]string{"withdrawal_id": w.ID.String(), "amount": amtStr(in.Amount), "currency": in.Currency},
	})
	go s.alertAdmins(ctx, "withdrawal", in.UserID, in.Provider, in.Currency, amtStr(in.Amount), in.RecipientPhone, "", in.Note)

	return &w, nil
}

func (s *ManualMomoService) ListUserWithdrawals(ctx context.Context, userID uuid.UUID, page, perPage int32) ([]db.ManualWithdrawal, error) {
	limit, offset := pageBounds(page, perPage)
	return db.New(s.pool).ListManualWithdrawalsByUser(ctx, db.ListManualWithdrawalsByUserParams{UserID: userID, Limit: limit, Offset: offset})
}

func (s *ManualMomoService) ListWithdrawalsByStatus(ctx context.Context, status string, page, perPage int32) ([]db.ManualWithdrawal, error) {
	limit, offset := pageBounds(page, perPage)
	return db.New(s.pool).ListManualWithdrawalsByStatus(ctx, db.ListManualWithdrawalsByStatusParams{Status: status, Lim: limit, Off: offset})
}

// CompleteWithdrawal marks a cash-out done after the admin has paid the recipient
// manually. The customer was already debited the full amount at request time;
// the charge just splits how much reaches the recipient (payout = amount - charge).
func (s *ManualMomoService) CompleteWithdrawal(ctx context.Context, withdrawalID, staffID uuid.UUID, charge float64, adminNote string) (*db.ManualWithdrawal, error) {
	if charge < 0 {
		charge = 0
	}
	q := db.New(s.pool)
	existing, err := q.GetManualWithdrawal(ctx, withdrawalID)
	if err != nil {
		return nil, ErrManualNotFound
	}
	if existing.Status != "pending" {
		return nil, ErrManualAlreadyReviewed
	}
	amount := parseFloatSafe(existing.Amount)
	if charge > amount+0.0001 {
		return nil, ErrMomoChargeTooHigh
	}
	payout := amount - charge
	chargeS, payoutS, note := amtStr(charge), amtStr(payout), strPtrOrNil(adminNote)
	w, err := q.CompleteManualWithdrawal(ctx, db.CompleteManualWithdrawalParams{
		ID: withdrawalID, Charge: &chargeS, PayoutAmount: &payoutS, AdminNote: note, ReviewedBy: staffPtr(staffID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrManualAlreadyReviewed
	}
	if err != nil {
		return nil, fmt.Errorf("complete manual withdrawal: %w", err)
	}
	if err := q.SetTransactionStatusByReference(ctx, db.SetTransactionStatusByReferenceParams{Reference: "MMW-" + w.ID.String(), Status: "completed"}); err != nil {
		s.logger.Warn("manual momo: mark withdrawal transaction completed failed", zap.String("withdrawal_id", w.ID.String()), zap.Error(err))
	}
	s.notif.Create(ctx, CreateNotificationInput{
		UserID: w.UserID,
		Type:   NotifManualWithdrawalCompleted,
		Title:  fmt.Sprintf("Cash-out sent — %s %s", payoutS, w.Currency),
		Body:   fmt.Sprintf("We've sent %s %s to %s.", payoutS, w.Currency, w.RecipientPhone),
		Metadata: map[string]string{"withdrawal_id": w.ID.String(), "payout": payoutS, "charge": chargeS, "currency": w.Currency},
	})
	return &w, nil
}

// RejectWithdrawal cancels a pending cash-out and refunds the held amount.
func (s *ManualMomoService) RejectWithdrawal(ctx context.Context, withdrawalID, staffID uuid.UUID, adminNote string) (*db.ManualWithdrawal, error) {
	q := db.New(s.pool)
	existing, err := q.GetManualWithdrawal(ctx, withdrawalID)
	if err != nil {
		return nil, ErrManualNotFound
	}
	if existing.Status != "pending" {
		return nil, ErrManualAlreadyReviewed
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin reject tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(tx)

	note := strPtrOrNil(adminNote)
	w, err := qtx.RejectManualWithdrawal(ctx, db.RejectManualWithdrawalParams{ID: withdrawalID, AdminNote: note, ReviewedBy: staffPtr(staffID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrManualAlreadyReviewed
	}
	if err != nil {
		return nil, fmt.Errorf("reject manual withdrawal: %w", err)
	}
	amt, err := numericFromFloat(parseFloatSafe(existing.Amount))
	if err != nil {
		return nil, fmt.Errorf("encode refund amount: %w", err)
	}
	if _, err := qtx.CreditAccount(ctx, db.CreditAccountParams{ID: w.AccountID, Balance: amt}); err != nil {
		return nil, fmt.Errorf("refund held funds: %w", err)
	}
	// Mark the pending debit transaction reversed (funds returned).
	if err := qtx.SetTransactionStatusByReference(ctx, db.SetTransactionStatusByReferenceParams{Reference: "MMW-" + w.ID.String(), Status: "reversed"}); err != nil {
		return nil, fmt.Errorf("reverse withdrawal transaction: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit reject tx: %w", err)
	}

	s.notif.Create(ctx, CreateNotificationInput{
		UserID: w.UserID,
		Type:   NotifManualWithdrawalRejected,
		Title:  "Cash-out cancelled — funds returned",
		Body:   fmt.Sprintf("Your cash-out of %s %s couldn't be completed. The amount has been returned to your %s balance.", existing.Amount, w.Currency, w.Currency),
		Metadata: map[string]string{"withdrawal_id": w.ID.String()},
	})
	return &w, nil
}

// ─── Admin email alert ────────────────────────────────────────────────────────

func (s *ManualMomoService) alertAdmins(ctx context.Context, kind string, userID uuid.UUID, provider, currency, amount, counterpart, reference, note string) {
	if s.emailClient == nil || s.adminEmail == "" {
		s.logger.Warn("manual momo: admin alert not sent — ADMIN_NOTIFY_EMAIL not configured",
			zap.String("kind", kind), zap.String("user_id", userID.String()))
		return
	}
	customer := userID.String()
	if u, err := db.New(s.pool).GetUserByID(ctx, userID); err == nil {
		customer = strings.TrimSpace(fmt.Sprintf("%s %s", u.FirstName, u.LastName))
		if u.Email != nil && *u.Email != "" {
			customer += " · " + *u.Email
		}
		customer += " · " + u.PhoneNumber
	}
	subject, html := email.ManualMomoAlert(s.adminEmail, email.ManualMomoAlertData{
		Kind: kind, Customer: customer, Provider: provider, Currency: currency, Amount: amount,
		Counterpart: counterpart, Reference: reference, Note: note,
		ReviewURL:   strings.TrimRight(s.appBaseURL, "/") + "/admin/momo/" + kind + "s",
		SubmittedAt: time.Now().UTC().Format("2006-01-02 15:04 UTC"),
	})
	if err := s.emailClient.Send(s.adminEmail, subject, html); err != nil {
		s.logger.Error("manual momo: admin alert email failed", zap.String("kind", kind), zap.Error(err))
	}
}

// staffPtr returns nil for the zero UUID so reviewed_by stores NULL (the owner has
// no admin_staff row and would otherwise violate the FK).
func staffPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// recordFiatTx writes a row to the canonical transactions table so the movement
// shows in the customer's activity feed / transaction history (which read only
// from that table). Called inside the same DB transaction as the ledger change
// so history and ledger stay consistent.
func recordFiatTx(ctx context.Context, q *db.Queries, accountID uuid.UUID, ref, txType, amount, currency, fee, description, status string, meta map[string]any) error {
	raw, _ := json.Marshal(meta)
	desc := description
	_, err := q.CreateFiatTransaction(ctx, db.CreateFiatTransactionParams{
		ID: uuid.New(), AccountID: accountID, Reference: ref, Type: txType,
		Amount: amount, Currency: currency, Fee: fee, Description: &desc, Status: status, Metadata: raw,
	})
	return err
}

// strPtrOrNil returns nil for an empty string so optional columns store NULL.
func strPtrOrNil(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// pageBounds normalises page/perPage into a LIMIT/OFFSET (1-based page, max 100).
func pageBounds(page, perPage int32) (limit, offset int32) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	return perPage, (page - 1) * perPage
}
