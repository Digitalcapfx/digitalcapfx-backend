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
	return q.GetAccountsByUserID(ctx, userID)
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
	return &acc, nil
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
