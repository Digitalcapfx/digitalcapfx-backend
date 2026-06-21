package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/caas"
	"github.com/rachfinance/digitalfx/internal/db/sqlc"
)

type CryptoService struct {
	pool       *pgxpool.Pool
	caasClient *caas.Client
	logger     *zap.Logger
}

func NewCryptoService(pool *pgxpool.Pool, caasClient *caas.Client, logger *zap.Logger) *CryptoService {
	return &CryptoService{pool: pool, caasClient: caasClient, logger: logger}
}

// GetOrCreateWallet returns a user's CaaS abstraction wallet, creating one if it doesn't exist.
// The wallet is an ERC-4337 smart account — the user's phone number is the identifier for P2P sends.
func (s *CryptoService) GetOrCreateWallet(ctx context.Context, userID uuid.UUID, phone string) (*db.CaasWallet, error) {
	q := db.New(s.pool)

	existing, err := q.GetCaasWalletByUserID(ctx, userID)
	if err == nil {
		return &existing, nil
	}

	resp, err := s.caasClient.CreateWallet(ctx, caas.CreateWalletRequest{
		UserID: userID.String(),
		Phone:  phone,
	})
	if err != nil {
		return nil, fmt.Errorf("caas create wallet: %w", err)
	}

	wallet, err := q.CreateCaasWallet(ctx, db.CreateCaasWalletParams{
		UserID:             userID,
		CaasWalletID:       resp.WalletID,
		AbstractionAddress: resp.AbstractionAddress,
	})
	if err != nil {
		return nil, fmt.Errorf("store caas wallet: %w", err)
	}

	return &wallet, nil
}

// GetBalances returns USDT and USDC balances for the user's abstraction wallet from CaaS.
func (s *CryptoService) GetBalances(ctx context.Context, userID uuid.UUID) (*caas.Balances, error) {
	q := db.New(s.pool)

	wallet, err := q.GetCaasWalletByUserID(ctx, userID)
	if err != nil {
		return nil, ErrAccountNotFound
	}

	return s.caasClient.GetBalances(ctx, wallet.AbstractionAddress)
}

type SendCryptoInput struct {
	SenderUserID uuid.UUID
	SenderPhone  string
	ReceiverPhone string
	Token        string // USDT | USDC
	Amount       string
}

// Send transfers USDT or USDC to another DigitalFX user identified by phone number.
// CaaS resolves the receiver's abstraction address from their phone number.
func (s *CryptoService) Send(ctx context.Context, in SendCryptoInput) (*db.CryptoTransaction, error) {
	q := db.New(s.pool)

	wallet, err := q.GetCaasWalletByUserID(ctx, in.SenderUserID)
	if err != nil {
		return nil, ErrAccountNotFound
	}

	// Resolve receiver's abstraction address from phone via CaaS
	receiverWallet, err := s.caasClient.ResolveByPhone(ctx, in.ReceiverPhone)
	if err != nil {
		return nil, fmt.Errorf("resolve receiver: %w", err)
	}

	// Submit the transfer to CaaS — it handles gas, bundler, and paymaster
	txResp, err := s.caasClient.Send(ctx, caas.SendRequest{
		FromAddress: wallet.AbstractionAddress,
		ToAddress:   receiverWallet.AbstractionAddress,
		Token:       in.Token,
		Amount:      in.Amount,
	})
	if err != nil {
		return nil, fmt.Errorf("caas send: %w", err)
	}

	// Resolve receiver user ID if they're a DigitalFX user
	var receiverUserID *uuid.UUID
	if receiverUser, err := q.GetUserByPhone(ctx, in.ReceiverPhone); err == nil {
		receiverUserID = &receiverUser.ID
	}

	ref := fmt.Sprintf("CRYPTO-%s", uuid.New().String()[:8])
	tx, err := q.CreateCryptoTransaction(ctx, db.CreateCryptoTransactionParams{
		Reference:       ref,
		SenderUserID:    in.SenderUserID,
		ReceiverPhone:   in.ReceiverPhone,
		ReceiverUserID:  receiverUserID,
		Token:           in.Token,
		Amount:          in.Amount,
		TxHash:          &txResp.TxHash,
		Status:          "processing",
	})
	if err != nil {
		return nil, fmt.Errorf("record transaction: %w", err)
	}

	return &tx, nil
}

func (s *CryptoService) ListTransactions(ctx context.Context, userID uuid.UUID, page, perPage int32) ([]db.CryptoTransaction, error) {
	q := db.New(s.pool)
	return q.ListCryptoTransactionsByUser(ctx, db.ListCryptoTransactionsByUserParams{
		SenderUserID:    userID,
		ReceiverUserID:  &userID,
		Limit:           perPage,
		Offset:          (page - 1) * perPage,
	})
}
