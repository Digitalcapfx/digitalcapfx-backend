package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rachfinance/digitalfx/internal/clients/hub2"
	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
	"go.uber.org/zap"
)

type VTUService struct {
	pool       *pgxpool.Pool
	hub2Client *hub2.Client
	logger     *zap.Logger
}

func NewVTUService(pool *pgxpool.Pool, hub2Client *hub2.Client, logger *zap.Logger) *VTUService {
	return &VTUService{
		pool:       pool,
		hub2Client: hub2Client,
		logger:     logger,
	}
}

func (s *VTUService) PurchaseAirtime(ctx context.Context, userID uuid.UUID, amount float64, currency, phone, operator string) (*db.VtuTransaction, error) {
	q := db.New(s.pool)
	// Find user account for this currency
	acc, err := q.GetAccountByUserAndCurrency(ctx, db.GetAccountByUserAndCurrencyParams{
		UserID:   userID,
		Currency: currency,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("no %s account found", currency)
		}
		return nil, err
	}

	// In a real flow, you'd wrap this in a DB transaction: 
	// 1. Check balance, 2. Debit account, 3. Call Hub2, 4. Insert VTU tx.
	
	// Mock Hub2 Call
	resp, err := s.hub2Client.PurchaseAirtime(ctx, hub2.PurchaseAirtimeRequest{
		Amount:   amount,
		Currency: currency,
		Phone:    phone,
		Operator: operator,
	})
	if err != nil {
		return nil, fmt.Errorf("hub2 api error: %w", err)
	}

	reference := fmt.Sprintf("VTU-%d", time.Now().UnixNano())

	// Create transaction record
	tx, err := q.CreateVTUTransaction(ctx, db.CreateVTUTransactionParams{
		UserID:        userID,
		AccountID:     acc.ID,
		Amount:        fmt.Sprintf("%.2f", amount),
		Currency:      currency,
		ServiceType:   "airtime",
		Provider:      "hub2",
		TargetPhone:   ptrString(phone),
		Reference:     ptrString(reference),
		ProviderRef:   ptrString(resp.Reference),
		Status:        resp.Status,
	})
	if err != nil {
		return nil, err
	}

	return &tx, nil
}

func (s *VTUService) PurchaseData(ctx context.Context, userID uuid.UUID, amount float64, currency, bundleID, phone, operator string) (*db.VtuTransaction, error) {
	q := db.New(s.pool)
	acc, err := q.GetAccountByUserAndCurrency(ctx, db.GetAccountByUserAndCurrencyParams{
		UserID:   userID,
		Currency: currency,
	})
	if err != nil {
		return nil, err
	}

	resp, err := s.hub2Client.PurchaseData(ctx, hub2.PurchaseDataRequest{
		BundleID: bundleID,
		Phone:    phone,
		Operator: operator,
	})
	if err != nil {
		return nil, err
	}

	reference := fmt.Sprintf("VTU-%d", time.Now().UnixNano())

	tx, err := q.CreateVTUTransaction(ctx, db.CreateVTUTransactionParams{
		UserID:        userID,
		AccountID:     acc.ID,
		Amount:        fmt.Sprintf("%.2f", amount),
		Currency:      currency,
		ServiceType:   "data",
		Provider:      "hub2",
		TargetPhone:   ptrString(phone),
		Reference:     ptrString(reference),
		ProviderRef:   ptrString(resp.Reference),
		Status:        resp.Status,
	})
	if err != nil {
		return nil, err
	}
	return &tx, nil
}

func (s *VTUService) PayBill(ctx context.Context, userID uuid.UUID, amount float64, currency, billerID, accountNumber string) (*db.VtuTransaction, error) {
	q := db.New(s.pool)
	acc, err := q.GetAccountByUserAndCurrency(ctx, db.GetAccountByUserAndCurrencyParams{
		UserID:   userID,
		Currency: currency,
	})
	if err != nil {
		return nil, err
	}

	resp, err := s.hub2Client.PayBill(ctx, hub2.PayBillRequest{
		BillerID:      billerID,
		AccountNumber: accountNumber,
		Amount:        amount,
	})
	if err != nil {
		return nil, err
	}

	reference := fmt.Sprintf("VTU-%d", time.Now().UnixNano())

	tx, err := q.CreateVTUTransaction(ctx, db.CreateVTUTransactionParams{
		UserID:        userID,
		AccountID:     acc.ID,
		Amount:        fmt.Sprintf("%.2f", amount),
		Currency:      currency,
		ServiceType:   "bill",
		Provider:      "hub2",
		TargetAccount: ptrString(accountNumber),
		Reference:     ptrString(reference),
		ProviderRef:   ptrString(resp.Reference),
		Status:        resp.Status,
	})
	if err != nil {
		return nil, err
	}
	return &tx, nil
}

func (s *VTUService) ListTransactions(ctx context.Context, userID uuid.UUID) ([]db.VtuTransaction, error) {
	q := db.New(s.pool)
	txs, err := q.ListVTUTransactionsByUserID(ctx, userID)
	if err != nil {
		s.logger.Error("failed to list vtu transactions", zap.Error(err))
		return nil, errors.New("failed to fetch transactions")
	}
	return txs, nil
}

