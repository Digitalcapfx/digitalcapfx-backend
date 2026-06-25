package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/hub2"
	"github.com/rachfinance/digitalfx/internal/clients/payments"
	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

type WalletService struct {
	pool           *pgxpool.Pool
	paymentsClient *payments.Client
	hub2Client     *hub2.Client
	logger         *zap.Logger
}

func NewWalletService(pool *pgxpool.Pool, paymentsClient *payments.Client, hub2Client *hub2.Client, logger *zap.Logger) *WalletService {
	return &WalletService{pool: pool, paymentsClient: paymentsClient, hub2Client: hub2Client, logger: logger}
}

// CreateWallet provisions an HD wallet for the user (if not already done) then
// derives a new address on the requested network.
//
// The Payments API WaaS model:
//   - One HD wallet (BIP-44 seed) per customer, identified by customer_id = userID.String().
//   - Unlimited addresses derived from that seed, keyed by (network, index).
//
// In the local DB, each derived address is a row in waas_wallets where
// waas_wallet_id holds the customer_id used in all future WaaS API calls.
func (s *WalletService) CreateWallet(ctx context.Context, userID uuid.UUID, network string) (*db.WaasWallet, error) {
	q := db.New(s.pool)
	customerID := userID.String()

	// Check whether we already have a derived address for this network.
	existing, err := q.GetWaasWalletByNetwork(ctx, db.GetWaasWalletByNetworkParams{
		UserID:  userID,
		Network: network,
	})
	if err == nil {
		return &existing, nil
	}

	// Ensure an HD wallet exists for this customer — idempotent on the API side;
	// a second call for the same customer_id returns the existing wallet without
	// a new mnemonic.
	_, err = s.paymentsClient.CreateCustomerWallet(ctx, payments.CreateCustomerWalletRequest{
		CustomerID: customerID,
		WordCount:  12,
	})
	if err != nil {
		// 409 / duplicate means the wallet already exists — that's fine, proceed.
		if apiErr, ok := err.(*payments.APIError); !ok || apiErr.Status != 409 {
			return nil, fmt.Errorf("payments api create customer wallet: %w", err)
		}
	}

	// Derive a fresh address on the requested network (index 0 by default; the
	// next free index is managed by the Payments API which auto-increments).
	derived, err := s.paymentsClient.DeriveAddress(ctx, customerID, payments.DeriveAddressRequest{
		Network:          payments.Network(network),
		EnableMonitoring: true,
	})
	if err != nil {
		return nil, fmt.Errorf("payments api derive address: %w", err)
	}

	wallet, err := q.CreateWaasWallet(ctx, db.CreateWaasWalletParams{
		UserID:       userID,
		WaasWalletID: customerID, // used in all future WaaS API calls
		Network:      derived.Network,
		Address:      derived.Address,
		IsDefault:    false,
	})
	if err != nil {
		return nil, fmt.Errorf("store waas wallet: %w", err)
	}

	return &wallet, nil
}

func (s *WalletService) ListWallets(ctx context.Context, userID uuid.UUID) ([]db.WaasWallet, error) {
	q := db.New(s.pool)
	return q.GetWaasWalletsByUserID(ctx, userID)
}

type DepositInput struct {
	UserID   uuid.UUID
	Currency string  // XAF | XOF
	Amount   float64
	Phone    string  // user's Mobile Money number (E.164)
	Operator string  // Orange | MTN | Wave | Moov | Airtel
}

// InitiateDeposit triggers a HUB2 Mobile Money collection request.
// HUB2 sends a push-to-pay prompt to the user's phone; on approval HUB2
// fires a webhook to /webhooks/hub2 which credits the user's CaaS USDC wallet.
func (s *WalletService) InitiateDeposit(ctx context.Context, in DepositInput) (string, error) {
	q := db.New(s.pool)

	resp, err := s.hub2Client.Collect(ctx, hub2.CollectRequest{
		Amount:      in.Amount,
		Currency:    in.Currency,
		Phone:       in.Phone,
		Operator:    in.Operator,
		Description: "DigitalFX deposit",
	})
	if err != nil {
		return "", fmt.Errorf("hub2 collect: %w", err)
	}

	// Persist so the webhook handler can look this up by reference.
	_, err = q.CreateHub2Payment(ctx, db.CreateHub2PaymentParams{
		Hub2Reference: resp.Reference,
		PaymentMethod: "mobile_money",
		Operator:      &in.Operator,
		PhoneNumber:   &in.Phone,
	})
	if err != nil {
		// Non-fatal: HUB2 accepted the request; log and return the reference.
		s.logger.Error("store hub2 deposit payment", zap.String("ref", resp.Reference), zap.Error(err))
	}

	return resp.Reference, nil
}

type WithdrawInput struct {
	UserID   uuid.UUID
	Currency string  // XAF | XOF
	Amount   float64
	Phone    string  // destination Mobile Money number (E.164)
	Operator string  // Orange | MTN | Wave | Moov | Airtel
}

// InitiateWithdrawal triggers a HUB2 Mobile Money disbursement to the user's phone.
// The user's CaaS USDC balance should be debited before calling this.
func (s *WalletService) InitiateWithdrawal(ctx context.Context, in WithdrawInput) (string, error) {
	q := db.New(s.pool)

	resp, err := s.hub2Client.Disburse(ctx, hub2.DisburseRequest{
		Amount:      in.Amount,
		Currency:    in.Currency,
		Phone:       in.Phone,
		Operator:    in.Operator,
		Description: "DigitalFX withdrawal",
	})
	if err != nil {
		return "", fmt.Errorf("hub2 disburse: %w", err)
	}

	_, err = q.CreateHub2Payment(ctx, db.CreateHub2PaymentParams{
		Hub2Reference: resp.Reference,
		PaymentMethod: "mobile_money",
		Operator:      &in.Operator,
		PhoneNumber:   &in.Phone,
	})
	if err != nil {
		s.logger.Error("store hub2 withdrawal payment", zap.String("ref", resp.Reference), zap.Error(err))
	}

	return resp.Reference, nil
}
