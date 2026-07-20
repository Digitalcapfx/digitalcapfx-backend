package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/db/sqlc"
)

var (
	ErrAccountNotFound     = errors.New("account not found")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrUnsupportedCurrency = errors.New("unsupported currency")
)

type AccountService struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

func NewAccountService(pool *pgxpool.Pool, logger *zap.Logger) *AccountService {
	return &AccountService{pool: pool, logger: logger}
}

func (s *AccountService) ListAccounts(ctx context.Context, userID uuid.UUID) ([]db.Account, error) {
	q := db.New(s.pool)
	accounts, err := q.GetAccountsByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if phone := s.userPhone(ctx, q, userID); phone != "" {
		for i := range accounts {
			if IsMobileMoneyCurrency(accounts[i].Currency) {
				accounts[i].AccountNumber = phone
			}
		}
	}
	return accounts, nil
}

func (s *AccountService) GetAccount(ctx context.Context, userID uuid.UUID, currency string) (*db.Account, error) {
	q := db.New(s.pool)
	acc, err := q.GetAccountByUserAndCurrency(ctx, db.GetAccountByUserAndCurrencyParams{
		UserID:   userID,
		Currency: currency,
	})
	if err != nil {
		return nil, ErrAccountNotFound
	}
	if IsMobileMoneyCurrency(acc.Currency) {
		if phone := s.userPhone(ctx, q, userID); phone != "" {
			acc.AccountNumber = phone
		}
	}
	return &acc, nil
}

// userPhone returns the user's phone number (best effort).
func (s *AccountService) userPhone(ctx context.Context, q *db.Queries, userID uuid.UUID) string {
	u, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return ""
	}
	return u.PhoneNumber
}

// IsMobileMoneyCurrency reports whether a fiat currency settles over mobile money
// (HUB2), where the account identifier is the user's phone number rather than a
// generated account number.
func IsMobileMoneyCurrency(currency string) bool {
	return currency == "XAF" || currency == "XOF"
}

// mobileMoneyAccountNumber returns the identifier to display for a fiat account:
// the phone number for HUB2 mobile-money currencies (XAF/XOF), otherwise the
// stored account number.
func mobileMoneyAccountNumber(currency, dbAccountNumber, phone string) string {
	if IsMobileMoneyCurrency(currency) && phone != "" {
		return phone
	}
	return dbAccountNumber
}

type ListTransactionsInput struct {
	AccountID uuid.UUID
	Page      int32
	PerPage   int32
}

type ListTransactionsResult struct {
	Transactions []db.Transaction
	Total        int64
}

func (s *AccountService) ListTransactions(ctx context.Context, in ListTransactionsInput) (*ListTransactionsResult, error) {
	q := db.New(s.pool)

	if in.PerPage == 0 {
		in.PerPage = 20
	}
	if in.Page == 0 {
		in.Page = 1
	}

	txs, err := q.ListTransactionsByAccount(ctx, db.ListTransactionsByAccountParams{
		AccountID: in.AccountID,
		Limit:     in.PerPage,
		Offset:    (in.Page - 1) * in.PerPage,
	})
	if err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}

	total, err := q.CountTransactionsByAccount(ctx, in.AccountID)
	if err != nil {
		return nil, fmt.Errorf("count transactions: %w", err)
	}

	return &ListTransactionsResult{Transactions: txs, Total: total}, nil
}

func (s *AccountService) GetTransaction(ctx context.Context, id uuid.UUID) (*db.Transaction, error) {
	q := db.New(s.pool)
	tx, err := q.GetTransactionByID(ctx, id)
	if err != nil {
		return nil, ErrAccountNotFound
	}
	return &tx, nil
}
