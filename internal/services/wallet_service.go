package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
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
	Currency string // XAF | XOF
	Amount   float64
	Phone    string // user's Mobile Money number (E.164)
	Operator string // Orange | MTN | Wave | Moov | Airtel
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

// GetSwapQuote requests a rate quote for swapping between tokens on WaaS.
func (s *WalletService) GetSwapQuote(ctx context.Context, fromChain, toChain, fromToken, toToken, amountIn string) (*payments.SwapQuoteResponse, error) {
	return s.paymentsClient.GetSwapQuote(ctx, payments.GetSwapQuoteParams{
		FromChain: fromChain,
		ToChain:   toChain,
		FromToken: fromToken,
		ToToken:   toToken,
		AmountIn:  amountIn,
	})
}

// ExecuteSwap triggers a swap transaction from the customer WaaS wallet.
func (s *WalletService) ExecuteSwap(ctx context.Context, userID uuid.UUID, fromChain, toChain, fromToken, toToken, amountIn, amountOutMin string) (*payments.ExecuteSwapResponse, error) {
	customerID := userID.String()
	return s.paymentsClient.ExecuteSwap(ctx, customerID, payments.ExecuteSwapRequest{
		FromChain:    fromChain,
		ToChain:      toChain,
		FromToken:    fromToken,
		ToToken:      toToken,
		AmountIn:     amountIn,
		AmountOutMin: amountOutMin,
	})
}

// GetSwapHistory returns a customer's swap transaction history logs.
func (s *WalletService) GetSwapHistory(ctx context.Context, userID uuid.UUID, page, limit int) (*payments.GetSwapHistoryResponse, error) {
	customerID := userID.String()
	return s.paymentsClient.GetSwapHistory(ctx, customerID, payments.GetSwapHistoryParams{
		Page:  page,
		Limit: limit,
	})
}

// GetWaasWallet retrieves a user's derived wallet details from the local DB.
func (s *WalletService) GetWaasWallet(ctx context.Context, id, userID uuid.UUID) (db.WaasWallet, error) {
	q := db.New(s.pool)
	return q.GetWaasWalletByIDAndUser(ctx, db.GetWaasWalletByIDAndUserParams{
		ID:     id,
		UserID: userID,
	})
}

// ExportPrivateKey exports the private key for a WaaS derived address.
func (s *WalletService) ExportPrivateKey(ctx context.Context, userID uuid.UUID, network string, index uint32) (*payments.ExportPrivateKeyResponse, error) {
	customerID := userID.String()
	return s.paymentsClient.ExportPrivateKey(ctx, customerID, payments.ExportPrivateKeyRequest{
		Network: payments.Network(network),
		Index:   index,
	})
}

// GetSeedPhrase reveals the mnemonic seed phrase of a user WaaS wallet.
func (s *WalletService) GetSeedPhrase(ctx context.Context, userID uuid.UUID) (*payments.GetSeedPhraseResponse, error) {
	customerID := userID.String()
	return s.paymentsClient.GetSeedPhrase(ctx, customerID)
}

// ListCustomerAddresses lists all derived addresses and their live balances.
func (s *WalletService) ListCustomerAddresses(ctx context.Context, userID uuid.UUID, refresh bool) (*payments.ListCustomerAddressesResponse, error) {
	customerID := userID.String()
	return s.paymentsClient.ListCustomerAddresses(ctx, customerID, refresh)
}

// EstimateGas calculates gas requirements for unauthenticated/public EVM transactions.
func (s *WalletService) EstimateGas(ctx context.Context, network, currency, fromAddress, toAddress, amount string) (*payments.GasEstimate, error) {
	return s.paymentsClient.EstimateGas(ctx, payments.EstimateGasRequest{
		Network:     payments.Network(network),
		Currency:    currency,
		FromAddress: fromAddress,
		ToAddress:   toAddress,
		Amount:      amount,
	})
}

// TransferCrypto sends crypto from the user's WaaS derived wallet to an
// external address. Amount is in the smallest on-chain unit (e.g. wei).
func (s *WalletService) TransferCrypto(ctx context.Context, userID uuid.UUID, network, currency, toAddress, amount string, index uint32) (*payments.TransferResponse, error) {
	customerID := userID.String()
	return s.paymentsClient.Transfer(ctx, customerID, payments.TransferRequest{
		Network:   payments.Network(network),
		Currency:  currency,
		ToAddress: toAddress,
		Amount:    amount,
		Index:     index,
	})
}

// GetWaasTransactions fetches the user's on-chain WaaS transaction history.
func (s *WalletService) GetWaasTransactions(ctx context.Context, userID uuid.UUID, page, limit int, network, currency, status string) (*payments.GetTransactionsResponse, error) {
	customerID := userID.String()
	return s.paymentsClient.GetTransactions(ctx, customerID, payments.GetTransactionsParams{
		Page:     page,
		Limit:    limit,
		Network:  payments.Network(network),
		Currency: currency,
		Status:   status,
	})
}

type WithdrawInput struct {
	UserID   uuid.UUID
	Currency string // XAF | XOF
	Amount   float64
	Phone    string // destination Mobile Money number (E.164)
	Operator string // Orange | MTN | Wave | Moov | Airtel
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

// GetWalletByAddress looks up a WaaS derived wallet by its on-chain address.
// Used by the payments deposit webhook to resolve the owning user.
func (s *WalletService) GetWalletByAddress(ctx context.Context, address string) (db.WaasWallet, error) {
	q := db.New(s.pool)
	return q.GetWaasWalletByAddress(ctx, address)
}

// CreditWaasDeposit credits a confirmed on-chain deposit to the user's fiat
// account for the given currency. amountCents is the amount in hundredths.
func (s *WalletService) CreditWaasDeposit(ctx context.Context, userID uuid.UUID, currency string, amountCents int64, txHash string) error {
	q := db.New(s.pool)

	account, err := q.GetAccountByUserAndCurrency(ctx, db.GetAccountByUserAndCurrencyParams{
		UserID:   userID,
		Currency: currency,
	})
	if err != nil {
		return fmt.Errorf("account not found for %s/%s: %w", userID, currency, err)
	}

	var amount pgtype.Numeric
	if err := amount.Scan(fmt.Sprintf("%d.%02d", amountCents/100, amountCents%100)); err != nil {
		return fmt.Errorf("encode amount: %w", err)
	}

	if _, err := q.CreditAccount(ctx, db.CreditAccountParams{
		ID:      account.ID,
		Balance: amount,
	}); err != nil {
		return fmt.Errorf("credit account %s: %w", account.ID, err)
	}

	s.logger.Info("waas deposit credited",
		zap.String("user_id", userID.String()),
		zap.String("currency", currency),
		zap.Int64("amount_cents", amountCents),
		zap.String("tx_hash", txHash),
	)
	return nil
}
