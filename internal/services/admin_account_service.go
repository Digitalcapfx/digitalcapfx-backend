package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

var (
	// ErrAccountStatusInvalid is returned for a status that is not active/inactive.
	ErrAccountStatusInvalid = errors.New("status must be 'active' or 'inactive'")
	// ErrBalanceModeInvalid is returned when a balance change mode is not set/adjust.
	ErrBalanceModeInvalid = errors.New("mode must be 'set' or 'adjust'")
	// ErrBalanceReasonRequired is returned when a balance change omits the reason.
	ErrBalanceReasonRequired = errors.New("a reason is required for balance changes")
)

// AdminAccountService is the admin-side management surface for customer ledger
// accounts: view, banking-detail edits, balance set/adjust, and per-currency
// enable/disable. Every mutation is written to the admin audit log; balance
// changes additionally record a ledger transaction so they appear in the
// customer's activity feed.
type AdminAccountService struct {
	pool   *pgxpool.Pool
	notif  *NotificationService
	logger *zap.Logger
}

func NewAdminAccountService(pool *pgxpool.Pool, notif *NotificationService, logger *zap.Logger) *AdminAccountService {
	return &AdminAccountService{pool: pool, notif: notif, logger: logger}
}

// ListCustomerAccounts returns every ledger account for a customer, across all
// currencies, including banking details, balances and status. Nothing is
// filtered — disabled ('inactive') accounts are returned too, carrying their
// status so the caller can decide visibility.
func (s *AdminAccountService) ListCustomerAccounts(ctx context.Context, userID uuid.UUID) ([]db.Account, error) {
	return db.New(s.pool).GetAccountsByUserID(ctx, userID)
}

func (s *AdminAccountService) getAccount(ctx context.Context, q *db.Queries, userID uuid.UUID, currency string) (db.Account, error) {
	return q.GetAccountByUserAndCurrency(ctx, db.GetAccountByUserAndCurrencyParams{
		UserID:   userID,
		Currency: strings.ToUpper(strings.TrimSpace(currency)),
	})
}

// ─── Enable / disable a currency for a customer ───────────────────────────────

// SetAccountStatus enables ('active') or disables ('inactive') one currency
// account for a customer. The account keeps being returned by the API; the
// status flag is what the frontend uses to show or hide it.
func (s *AdminAccountService) SetAccountStatus(ctx context.Context, set StaffPermissionSet, userID uuid.UUID, currency string, enabled bool, ip string) (*db.Account, error) {
	q := db.New(s.pool)
	acc, err := s.getAccount(ctx, q, userID, currency)
	if err != nil {
		return nil, ErrAccountNotFound
	}
	status := "inactive"
	if enabled {
		status = "active"
	}
	updated, err := q.SetAccountStatusByID(ctx, db.SetAccountStatusByIDParams{ID: acc.ID, Status: status})
	if err != nil {
		return nil, fmt.Errorf("set account status: %w", err)
	}
	s.audit(ctx, set, "account.status", acc.ID.String(), ip, map[string]any{
		"user_id": userID.String(), "currency": updated.Currency, "status": status,
	})
	return &updated, nil
}

// ─── Banking details ──────────────────────────────────────────────────────────

type UpdateBankingInput struct {
	AccountNumber          *string
	Iban                   *string
	Bic                    *string
	SortCode               *string
	AccountNumberUK        *string
	NombaBankName          *string
	NombaBankAccountNumber *string
	NombaBankAccountName   *string
}

// UpdateBanking applies a partial update of a customer account's banking details.
func (s *AdminAccountService) UpdateBanking(ctx context.Context, set StaffPermissionSet, userID uuid.UUID, currency string, in UpdateBankingInput, ip string) (*db.Account, error) {
	q := db.New(s.pool)
	acc, err := s.getAccount(ctx, q, userID, currency)
	if err != nil {
		return nil, ErrAccountNotFound
	}
	updated, err := q.AdminUpdateAccountBanking(ctx, db.AdminUpdateAccountBankingParams{
		ID:                     acc.ID,
		AccountNumber:          in.AccountNumber,
		Iban:                   in.Iban,
		Bic:                    in.Bic,
		SortCode:               in.SortCode,
		AccountNumberUk:        in.AccountNumberUK,
		NombaBankName:          in.NombaBankName,
		NombaBankAccountNumber: in.NombaBankAccountNumber,
		NombaBankAccountName:   in.NombaBankAccountName,
	})
	if err != nil {
		return nil, fmt.Errorf("update banking: %w", err)
	}
	s.audit(ctx, set, "account.banking", acc.ID.String(), ip, map[string]any{
		"user_id": userID.String(), "currency": updated.Currency,
		"fields": bankingChangedFields(in),
	})
	return &updated, nil
}

// ─── Balance (set / adjust) ───────────────────────────────────────────────────

type BalanceChangeInput struct {
	Mode   string  // "set" | "adjust"
	Amount float64 // set: new absolute balance; adjust: signed delta
	Reason string
}

type BalanceChangeResult struct {
	Account       db.Account `json:"account"`
	PreviousBalance float64  `json:"previous_balance"`
	NewBalance    float64    `json:"new_balance"`
	Delta         float64    `json:"delta"`
	Reference     string     `json:"reference"`
}

// SetOrAdjustBalance sets an absolute balance or applies a signed delta to a
// customer's account. Requires a reason, is audit-logged, and records a matching
// ledger transaction (credit/debit) so the change surfaces in the customer's
// activity feed. The customer is notified in-app.
func (s *AdminAccountService) SetOrAdjustBalance(ctx context.Context, set StaffPermissionSet, userID uuid.UUID, currency string, in BalanceChangeInput, ip string) (*BalanceChangeResult, error) {
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode != "set" && mode != "adjust" {
		return nil, ErrBalanceModeInvalid
	}
	if strings.TrimSpace(in.Reason) == "" {
		return nil, ErrBalanceReasonRequired
	}

	q := db.New(s.pool)
	acc, err := s.getAccount(ctx, q, userID, currency)
	if err != nil {
		return nil, ErrAccountNotFound
	}
	prev := pgNumericToFloat(acc.Balance)

	var delta, newBal float64
	if mode == "set" {
		newBal = in.Amount
		delta = newBal - prev
	} else {
		delta = in.Amount
		newBal = prev + delta
	}

	var updated db.Account
	if mode == "set" {
		amt, nerr := numericFromFloat(newBal)
		if nerr != nil {
			return nil, fmt.Errorf("encode amount: %w", nerr)
		}
		updated, err = q.SetAccountBalanceByID(ctx, db.SetAccountBalanceByIDParams{ID: acc.ID, Balance: amt})
	} else {
		amt, nerr := numericFromFloat(delta)
		if nerr != nil {
			return nil, fmt.Errorf("encode amount: %w", nerr)
		}
		updated, err = q.AdjustAccountBalanceByID(ctx, db.AdjustAccountBalanceByIDParams{ID: acc.ID, Balance: amt})
	}
	if err != nil {
		return nil, fmt.Errorf("apply balance change: %w", err)
	}

	reference := "ADJ-" + uuid.NewString()
	// Record a ledger transaction for the movement so it appears in activity.
	if delta != 0 {
		txType := "credit"
		if delta < 0 {
			txType = "debit"
		}
		absDelta := delta
		if absDelta < 0 {
			absDelta = -absDelta
		}
		desc := "Admin balance adjustment — " + strings.TrimSpace(in.Reason)
		meta := map[string]any{
			"admin_adjustment": true, "mode": mode, "reason": in.Reason,
			"previous_balance": fmt.Sprintf("%.2f", prev), "new_balance": fmt.Sprintf("%.2f", newBal),
			"staff_email": set.StaffEmail,
		}
		if rerr := recordFiatTx(ctx, q, acc.ID, reference, txType,
			fmt.Sprintf("%.2f", absDelta), updated.Currency, "0.00", desc, "completed", meta); rerr != nil {
			s.logger.Warn("admin balance: record ledger tx failed", zap.String("account", acc.ID.String()), zap.Error(rerr))
		}
	}

	s.audit(ctx, set, "account.balance", acc.ID.String(), ip, map[string]any{
		"user_id": userID.String(), "currency": updated.Currency, "mode": mode,
		"previous_balance": prev, "new_balance": newBal, "delta": delta, "reason": in.Reason,
	})

	// Notify the customer (in-app + push). Not emailed by default to avoid alarm.
	if delta != 0 {
		verb := "credited to"
		if delta < 0 {
			verb = "debited from"
		}
		absDelta := delta
		if absDelta < 0 {
			absDelta = -absDelta
		}
		s.notif.Create(ctx, CreateNotificationInput{
			UserID: userID,
			Type:   NotifBalanceAdjusted,
			Title:  fmt.Sprintf("%s balance updated", updated.Currency),
			Body:   fmt.Sprintf("%.2f %s was %s your %s account.", absDelta, updated.Currency, verb, updated.Currency),
			Metadata: map[string]string{
				"currency": updated.Currency, "reference": reference,
				"new_balance": fmt.Sprintf("%.2f", newBal),
			},
		})
	}

	return &BalanceChangeResult{
		Account: updated, PreviousBalance: prev, NewBalance: newBal, Delta: delta, Reference: reference,
	}, nil
}

// ─── Audit helper ─────────────────────────────────────────────────────────────

// audit writes an admin audit-log entry. The owner has no admin_staff row, so
// staff_id is stored NULL while staff_name/staff_email still identify the actor.
func (s *AdminAccountService) audit(ctx context.Context, set StaffPermissionSet, action, resourceID, ip string, details map[string]any) {
	raw, _ := json.Marshal(details)
	var staffID *uuid.UUID
	if id, perr := uuid.Parse(set.StaffID); perr == nil && id != uuid.Nil {
		staffID = &id
	}
	rid := resourceID
	ipp := ip
	if _, err := db.New(s.pool).CreateAdminAuditLog(ctx, db.CreateAdminAuditLogParams{
		StaffID:    staffID,
		StaffName:  set.StaffName,
		StaffEmail: set.StaffEmail,
		Action:     action,
		Resource:   "accounts",
		ResourceID: &rid,
		Details:    raw,
		IPAddress:  &ipp,
	}); err != nil {
		s.logger.Warn("admin account audit write failed", zap.String("action", action), zap.Error(err))
	}
}

func bankingChangedFields(in UpdateBankingInput) []string {
	var f []string
	if in.AccountNumber != nil {
		f = append(f, "account_number")
	}
	if in.Iban != nil {
		f = append(f, "iban")
	}
	if in.Bic != nil {
		f = append(f, "bic")
	}
	if in.SortCode != nil {
		f = append(f, "sort_code")
	}
	if in.AccountNumberUK != nil {
		f = append(f, "account_number_uk")
	}
	if in.NombaBankName != nil {
		f = append(f, "nomba_bank_name")
	}
	if in.NombaBankAccountNumber != nil {
		f = append(f, "nomba_bank_account_number")
	}
	if in.NombaBankAccountName != nil {
		f = append(f, "nomba_bank_account_name")
	}
	return f
}
