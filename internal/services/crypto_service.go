package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/caas"
	"github.com/rachfinance/digitalfx/internal/clients/hub2"
	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

type CryptoService struct {
	pool       *pgxpool.Pool
	caasClient *caas.Client
	hub2Client *hub2.Client
	logger     *zap.Logger
}

func NewCryptoService(pool *pgxpool.Pool, caasClient *caas.Client, hub2Client *hub2.Client, logger *zap.Logger) *CryptoService {
	return &CryptoService{pool: pool, caasClient: caasClient, hub2Client: hub2Client, logger: logger}
}

// ─── Step 3: Create Instant USD Account ──────────────────────────────────────

// GetOrCreateWallet provisions an ERC-4337 SCW for the user via CaaS and
// caches the wallet address in the local DB.
func (s *CryptoService) GetOrCreateWallet(ctx context.Context, userID uuid.UUID, phone string) (*db.CaasWallet, error) {
	q := db.New(s.pool)

	existing, err := q.GetCaasWalletByUserID(ctx, userID)
	if err == nil {
		return &existing, nil
	}

	resp, err := s.caasClient.ProvisionSCW(ctx, caas.ProvisionSCWRequest{
		PhoneNumber: phone,
	})
	if err != nil {
		return nil, fmt.Errorf("caas provision scw: %w", err)
	}

	blindIndex := resp.BlindIndex
	wallet, err := q.CreateCaasWalletFull(ctx, db.CreateCaasWalletFullParams{
		UserID:             userID,
		CaasWalletID:       resp.BlindIndex,
		BlindIndex:         &blindIndex,
		AbstractionAddress: resp.WalletAddress,
	})
	if err != nil {
		return nil, fmt.Errorf("store caas wallet: %w", err)
	}

	return &wallet, nil
}

// ─── Step 4–5: Fund Instant USD Account via Mobile Money ─────────────────────

type FundingInput struct {
	UserID   uuid.UUID
	Currency string     // XOF | XAF — the fiat being collected via Mobile Money
	Amount   float64    // amount in local fiat
	Phone    string     // Mobile Money number to pull from (E.164)
	Operator string     // Orange | MTN | Wave | Moov | Airtel
	Token    caas.Token // USDC | USDT — the stablecoin to receive in the SCW
}

// InitiateFunding starts the customer-triggered flow to top up their Instant USD Account:
//
//  1. Ensures the user's ERC-4337 SCW is provisioned (idempotent).
//  2. Calls HUB2 Collect — HUB2 sends a push-to-pay prompt to the customer's phone.
//  3. Stores the Hub2Payment so the webhook handler can reconcile it later.
//
// The flow completes asynchronously:
//   - HUB2 fires POST /webhooks/hub2 when the customer approves.
//   - HUB2Service.HandleWebhook calls CaaS FundUser to notify CaaS of the incoming XOF.
//   - DigitalFX physically transfers XOF to Rach CaaS's bank account (Ivory Coast).
//   - Rach CaaS confirms fiat receipt, converts via OTC partners, credits the SCW.
//   - Customer sees updated balance via GET /crypto/balances (live from CaaS).
func (s *CryptoService) InitiateFunding(ctx context.Context, in FundingInput) (string, error) {
	q := db.New(s.pool)

	// Get user to confirm they exist and to get their canonical phone for CaaS.
	user, err := q.GetUserByID(ctx, in.UserID)
	if err != nil {
		return "", ErrAccountNotFound
	}

	// Ensure SCW is provisioned before collecting fiat — we never want to collect
	// money for a user whose wallet doesn't exist yet in CaaS.
	if _, err := s.caasClient.ProvisionSCW(ctx, caas.ProvisionSCWRequest{
		PhoneNumber: user.PhoneNumber,
	}); err != nil {
		return "", fmt.Errorf("caas provision scw before collect: %w", err)
	}

	// Initiate Mobile Money collection via HUB2. The customer receives a
	// push-to-pay prompt and must approve within HUB2's timeout window.
	collectResp, err := s.hub2Client.Collect(ctx, hub2.CollectRequest{
		Amount:      in.Amount,
		Currency:    in.Currency,
		Phone:       in.Phone,
		Operator:    in.Operator,
		Description: fmt.Sprintf("DigitalFX — Fund Instant %s Account", string(in.Token)),
	})
	if err != nil {
		return "", fmt.Errorf("hub2 collect: %w", err)
	}

	// Store the Hub2Payment keyed on the reference. HandleWebhook will look this
	// up when HUB2 fires the status update, and use the PhoneNumber to identify
	// which user's SCW to credit.
	_, err = q.CreateHub2Payment(ctx, db.CreateHub2PaymentParams{
		Hub2Reference: collectResp.Reference,
		PaymentMethod: "mobile_money",
		Operator:      &in.Operator,
		PhoneNumber:   &in.Phone,
	})
	if err != nil {
		// Non-fatal: the HUB2 collection is already in flight. Log and return.
		s.logger.Error("store hub2 funding payment",
			zap.String("ref", collectResp.Reference),
			zap.Error(err),
		)
	}

	s.logger.Info("instant USD account funding initiated",
		zap.String("reference", collectResp.Reference),
		zap.String("user_id", in.UserID.String()),
		zap.String("mm_phone", in.Phone),
		zap.Float64("amount", in.Amount),
		zap.String("currency", in.Currency),
		zap.String("target_token", string(in.Token)),
	)

	return collectResp.Reference, nil
}

// ─── Step 9: Balance — the source of truth ────────────────────────────────────

// GetBalances returns the live USDC balance for the user's SCW.
// CaaS only returns USDC — USDT is not currently supported in the settlement engine.
func (s *CryptoService) GetBalances(ctx context.Context, userID uuid.UUID) (*caas.BalanceResponse, error) {
	q := db.New(s.pool)

	// Look up user to get their phone number (CaaS identifies users by phone).
	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return nil, ErrAccountNotFound
	}

	return s.caasClient.GetBalance(ctx, user.PhoneNumber)
}

type SendCryptoInput struct {
	SenderUserID  uuid.UUID
	SenderPhone   string
	ReceiverPhone string
	Token         string // USDT | USDC
	Amount        string // decimal string e.g. "10.50"
}

// Send executes a gasless peer-to-peer stablecoin transfer between two DigitalFX
// users identified by their phone numbers.
//
// Flow:
//  1. Submit transfer to CaaS using a DIRECT_CRYPTO quote_id — no FX conversion
//     needed for USDC-to-USDC (1:1 with USD), which avoids an extra round-trip.
//  2. Record the pending transaction locally; status updated via webhook.
func (s *CryptoService) Send(ctx context.Context, in SendCryptoInput) (*db.CryptoTransaction, error) {
	q := db.New(s.pool)

	// DIRECT_CRYPTO:{TOKEN}:{AMOUNT} bypasses the FX quote step for same-token
	// transfers. USDC ≈ USD 1:1 so no rate conversion is required.
	directQuoteID := fmt.Sprintf("DIRECT_CRYPTO:%s:%s", in.Token, in.Amount)

	ref := fmt.Sprintf("CRYPTO-%s", uuid.New().String()[:8])
	txResp, err := s.caasClient.Transfer(ctx, caas.TransferRequest{
		IdempotencyKey:  ref,
		SenderPhone:     in.SenderPhone,
		RecipientPhone:  in.ReceiverPhone,
		LocalFiatAmount: in.Amount, // USDC amount (USD ≈ USDC, no conversion)
		QuoteID:         directQuoteID,
		TargetToken:     caas.Token(in.Token),
	})
	if err != nil {
		return nil, fmt.Errorf("caas transfer: %w", err)
	}

	// Resolve receiver's local user ID if they're on DigitalFX.
	var receiverUserID *uuid.UUID
	if receiverUser, err := q.GetUserByPhoneAny(ctx, phoneCandidates(in.ReceiverPhone)); err == nil {
		receiverUserID = &receiverUser.ID
	}

	transferID := txResp.TransferID
	localCurrency := "USD"
	tx, err := q.CreateCryptoTransaction(ctx, db.CreateCryptoTransactionParams{
		Reference:       ref,
		SenderUserID:    in.SenderUserID,
		ReceiverPhone:   in.ReceiverPhone,
		ReceiverUserID:  receiverUserID,
		Token:           in.Token,
		Amount:          in.Amount,
		TxHash:          &transferID, // CaaS transfer_id — on-chain tx_hash resolved async
		Status:          txResp.Status,
		QuoteID:         &directQuoteID,
		CaasTransferID:  &transferID,
		IdempotencyKey:  &ref,
		LocalFiatAmount: &in.Amount,
		LocalCurrency:   &localCurrency,
	})
	if err != nil {
		return nil, fmt.Errorf("record crypto transaction: %w", err)
	}

	return &tx, nil
}

func (s *CryptoService) ListTransactions(ctx context.Context, userID uuid.UUID, page, perPage int32) ([]db.CryptoTransaction, error) {
	q := db.New(s.pool)
	return q.ListCryptoTransactionsByUser(ctx, db.ListCryptoTransactionsByUserParams{
		SenderUserID:   userID,
		ReceiverUserID: &userID,
		Limit:          perPage,
		Offset:         (page - 1) * perPage,
	})
}

type WithdrawCryptoInput struct {
	UserID         uuid.UUID
	Phone          string // E.164 — user's SCW owner phone
	Amount         string // stablecoin amount e.g. "50.00"
	Token          caas.Token
	PayoutMobile   string // Mobile Money destination number (E.164)
	PayoutNetwork  string // Orange | MTN | Wave | Moov | Airtel
	IdempotencyKey string // caller-supplied; use a stable UUID per withdrawal request
}

// Withdraw initiates a stablecoin off-ramp from the user's SCW to a Mobile Money number.
// CaaS debits the SCW and disburses fiat asynchronously via the payout_network operator.
func (s *CryptoService) Withdraw(ctx context.Context, in WithdrawCryptoInput) (*db.CaasWithdrawal, error) {
	q := db.New(s.pool)

	// Idempotency guard — return existing record if already submitted.
	if existing, err := q.GetCaasWithdrawalByIdempotencyKey(ctx, in.IdempotencyKey); err == nil {
		return &existing, nil
	}

	resp, err := s.caasClient.Withdraw(ctx, caas.WithdrawRequest{
		Phone:          in.Phone,
		Amount:         in.Amount,
		Token:          in.Token,
		PayoutMobile:   in.PayoutMobile,
		PayoutNetwork:  in.PayoutNetwork,
		IdempotencyKey: in.IdempotencyKey,
	})
	if err != nil {
		return nil, fmt.Errorf("caas withdraw: %w", err)
	}

	withdrawal, err := q.CreateCaasWithdrawal(ctx, db.CreateCaasWithdrawalParams{
		UserID:           in.UserID,
		CaasWithdrawalID: resp.WithdrawalID,
		Phone:            in.Phone,
		Amount:           in.Amount,
		Token:            string(in.Token),
		PayoutMobile:     in.PayoutMobile,
		PayoutNetwork:    in.PayoutNetwork,
		IdempotencyKey:   in.IdempotencyKey,
	})
	if err != nil {
		s.logger.Error("failed to record caas withdrawal locally",
			zap.String("withdrawal_id", resp.WithdrawalID), zap.Error(err))
		return nil, nil
	}

	s.logger.Info("caas withdrawal initiated",
		zap.String("withdrawal_id", resp.WithdrawalID),
		zap.String("phone", in.Phone),
		zap.String("amount", in.Amount),
		zap.String("token", string(in.Token)),
	)
	return &withdrawal, nil
}

// UpdatePhone re-links the user's SCW to a new phone number on CaaS and refreshes
// the local blind_index. Call this when a user changes their registered phone.
func (s *CryptoService) UpdatePhone(ctx context.Context, userID uuid.UUID, oldPhone, newPhone string) error {
	q := db.New(s.pool)

	resp, err := s.caasClient.UpdatePhone(ctx, caas.UpdatePhoneRequest{
		OldPhoneNumber: oldPhone,
		NewPhoneNumber: newPhone,
	})
	if err != nil {
		return fmt.Errorf("caas update-phone: %w", err)
	}

	newBlindIndex := resp.BlindIndex
	if err := q.UpdateCaasWalletPhone(ctx, db.UpdateCaasWalletPhoneParams{
		UserID:     userID,
		BlindIndex: &newBlindIndex,
	}); err != nil {
		s.logger.Error("failed to update local blind_index after phone change",
			zap.String("user_id", userID.String()), zap.Error(err))
	}

	return nil
}
