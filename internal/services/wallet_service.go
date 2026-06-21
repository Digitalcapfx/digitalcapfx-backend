package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/payments"
	"github.com/rachfinance/digitalfx/internal/db/sqlc"
)

type WalletService struct {
	pool           *pgxpool.Pool
	paymentsClient *payments.Client
	logger         *zap.Logger
}

func NewWalletService(pool *pgxpool.Pool, paymentsClient *payments.Client, logger *zap.Logger) *WalletService {
	return &WalletService{pool: pool, paymentsClient: paymentsClient, logger: logger}
}

// CreateWallet provisions a new custody wallet for a given network via the Payments API (WaaS).
func (s *WalletService) CreateWallet(ctx context.Context, userID uuid.UUID, network string) (*db.WaasWallet, error) {
	q := db.New(s.pool)

	resp, err := s.paymentsClient.CreateWallet(ctx, payments.CreateWalletRequest{
		UserID:  userID.String(),
		Network: network,
	})
	if err != nil {
		return nil, fmt.Errorf("payments api create wallet: %w", err)
	}

	wallet, err := q.CreateWaasWallet(ctx, db.CreateWaasWalletParams{
		UserID:       userID,
		WaasWalletID: resp.WalletID,
		Network:      network,
		Address:      resp.Address,
		IsDefault:    false,
	})
	if err != nil {
		return nil, fmt.Errorf("store wallet: %w", err)
	}

	return &wallet, nil
}

func (s *WalletService) ListWallets(ctx context.Context, userID uuid.UUID) ([]db.WaasWallet, error) {
	q := db.New(s.pool)
	return q.GetWaasWalletsByUserID(ctx, userID)
}

type DepositInput struct {
	UserID      uuid.UUID
	Currency    string
	Amount      float64
	Phone       string
	Operator    string
	PaymentMethod string
}

// InitiateDeposit creates a HUB2 collection request for fiat deposit (XAF/XOF).
// The resulting hub2_reference is used to reconcile the webhook callback.
func (s *WalletService) InitiateDeposit(ctx context.Context, in DepositInput) (string, error) {
	// Implemented when we wire HUB2 service end-to-end
	return "", fmt.Errorf("not implemented")
}

type WithdrawInput struct {
	UserID   uuid.UUID
	Currency string
	Amount   float64
	Phone    string
	Operator string
}

// InitiateWithdrawal creates a HUB2 disbursement request for fiat withdrawal.
func (s *WalletService) InitiateWithdrawal(ctx context.Context, in WithdrawInput) (string, error) {
	return "", fmt.Errorf("not implemented")
}
