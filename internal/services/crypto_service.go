package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/caas"
	"github.com/rachfinance/digitalfx/internal/clients/hub2"
	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

var (
	// ErrUnsupportedToken is returned when a token is not USDC or USDT.
	ErrUnsupportedToken = errors.New("token must be USDC or USDT")
	// ErrSameSwapToken is returned when token_in equals token_out.
	ErrSameSwapToken = errors.New("token_in and token_out must differ")
	// ErrSwapInvalidAmount is returned for a missing or non-positive swap amount.
	ErrSwapInvalidAmount = errors.New("amount must be greater than zero")
	// ErrSwapNotFound is returned when a swap does not exist or is not owned by the caller.
	ErrSwapNotFound = errors.New("swap not found")
	// ErrTransferNotFound is returned when a CaaS transfer cannot be matched to a local record.
	ErrTransferNotFound = errors.New("transfer not found")
)

// isStableToken reports whether t is one of the two supported CaaS stablecoins.
func isStableToken(t string) bool { return t == "USDC" || t == "USDT" }

// The CaaS balance settles on-chain as USDC. It is presented to customers as
// USDC with the human-readable name "Stablecoin" (previously branded "iUSD" /
// "Instant USD"). This is the Rach CaaS balance — the EIP-4337 SCW rail.
const (
	StablecoinSymbol = "USDC"
	StablecoinName   = "Stablecoin"
)

type CaaSService struct {
	pool       *pgxpool.Pool
	caasClient *caas.Client
	hub2Client *hub2.Client
	logger     *zap.Logger
}

func NewCaaSService(pool *pgxpool.Pool, caasClient *caas.Client, hub2Client *hub2.Client, logger *zap.Logger) *CaaSService {
	return &CaaSService{pool: pool, caasClient: caasClient, hub2Client: hub2Client, logger: logger}
}

// ─── Step 3: Create Instant USD Account ──────────────────────────────────────

// GetOrCreateWallet provisions an ERC-4337 SCW for the user via CaaS and
// caches the wallet address in the local DB.
func (s *CaaSService) GetOrCreateWallet(ctx context.Context, userID uuid.UUID, phone string) (*db.CaasWallet, error) {
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
func (s *CaaSService) InitiateFunding(ctx context.Context, in FundingInput) (string, error) {
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
// InstantUSDBalance is the customer-facing CaaS balance. The value settles
// on-chain as USDC and is presented as USDC (name "Stablecoin"). balance_usdc is
// kept for backward compatibility.
// StablecoinToken is one stablecoin balance on the customer's SCW (USDC or USDT).
type StablecoinToken struct {
	Symbol           string  `json:"symbol"` // "USDC" | "USDT"
	Name             string  `json:"name"`   // "Stablecoin"
	Balance          float64 `json:"balance"`
	BalanceRaw       string  `json:"balance_raw"` // decimal string
	BalanceFormatted string  `json:"balance_formatted"`
}

type InstantUSDBalance struct {
	// Tokens holds the per-stablecoin balances (USDC and USDT). CaaS supports both
	// equally — prefer this over the flat fields below.
	Tokens        []StablecoinToken `json:"tokens"`
	WalletAddress string            `json:"wallet_address"` // always present (from our caas_wallets record)

	// Flat fields retained for backward compatibility (USDC).
	Symbol           string  `json:"symbol"`       // "USDC"
	Name             string  `json:"name"`         // "Stablecoin"
	Balance          float64 `json:"balance"`      // numeric USDC balance
	BalanceUSDC      string  `json:"balance_usdc"` // raw on-chain USDC
	BalanceUSDT      string  `json:"balance_usdt"` // raw on-chain USDT
	BalanceFormatted string  `json:"balance_formatted"`
}

func (s *CaaSService) GetBalances(ctx context.Context, userID uuid.UUID) (*InstantUSDBalance, error) {
	q := db.New(s.pool)

	// Look up user to get their phone number (CaaS identifies users by phone).
	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return nil, ErrAccountNotFound
	}

	out := &InstantUSDBalance{Symbol: StablecoinSymbol, Name: StablecoinName, BalanceUSDC: "0", BalanceUSDT: "0"}

	// Ensure the user's SCW is provisioned (idempotent — only calls CaaS when we
	// have no local record). This guarantees wallet_address is always present and
	// resolves the "Wallet not found" 404 that CaaS returns on the balance read
	// for a user who was never provisioned. The address is the deterministic SCW
	// address, so it is stable across calls.
	if wallet, werr := s.GetOrCreateWallet(ctx, userID, user.PhoneNumber); werr == nil {
		out.WalletAddress = wallet.AbstractionAddress
	} else {
		s.logger.Warn("crypto balances: ensure wallet failed",
			zap.String("user_id", userID.String()), zap.Error(werr))
	}

	// Best-effort: an unfunded / freshly-provisioned SCW should read as 0 USDC,
	// not 500.
	if bal, berr := s.caasClient.GetBalance(ctx, user.PhoneNumber); berr == nil {
		if bal.BalanceUSDC != "" {
			out.BalanceUSDC = bal.BalanceUSDC
		}
		if bal.BalanceUSDT != "" {
			out.BalanceUSDT = bal.BalanceUSDT
		}
		if bal.WalletAddress != "" {
			out.WalletAddress = bal.WalletAddress
		}
	} else {
		s.logger.Warn("crypto balances: caas balance unavailable, returning 0",
			zap.String("user_id", userID.String()), zap.Error(berr))
	}

	usdc, usdt := parseFloatSafe(out.BalanceUSDC), parseFloatSafe(out.BalanceUSDT)
	out.Balance = usdc // back-compat: flat balance is USDC
	out.BalanceFormatted = fmt.Sprintf("%.2f %s", usdc, StablecoinSymbol)
	out.Tokens = []StablecoinToken{
		{Symbol: "USDC", Name: StablecoinName, Balance: usdc, BalanceRaw: out.BalanceUSDC, BalanceFormatted: fmt.Sprintf("%.2f USDC", usdc)},
		{Symbol: "USDT", Name: StablecoinName, Balance: usdt, BalanceRaw: out.BalanceUSDT, BalanceFormatted: fmt.Sprintf("%.2f USDT", usdt)},
	}
	return out, nil
}

type SendCryptoInput struct {
	SenderUserID uuid.UUID
	SenderPhone  string
	// Recipient is addressed by EITHER phone OR wallet address — set exactly one.
	// ReceiverPhone is an E.164 phone number; ReceiverAddress is a 0x SCW address.
	ReceiverPhone   string
	ReceiverAddress string
	Token           string // USDT | USDC
	Amount          string // decimal string e.g. "10.50"
}

// Send executes a gasless peer-to-peer stablecoin transfer from a DigitalFX user
// to a recipient identified by EITHER a phone number OR a raw 0x SCW wallet
// address. Rach CaaS auto-detects the identifier format on the recipient field,
// so we forward whichever one the caller supplied.
//
// Flow:
//  1. Submit transfer to CaaS using a DIRECT_CRYPTO quote_id — no FX conversion
//     needed for USDC-to-USDC (1:1 with USD), which avoids an extra round-trip.
//  2. Record the pending transaction locally; status updated via webhook.
func (s *CaaSService) Send(ctx context.Context, in SendCryptoInput) (*db.CryptoTransaction, error) {
	q := db.New(s.pool)

	// The CaaS recipient field auto-detects phone vs. 0x address; forward whichever
	// identifier the caller supplied (address takes precedence when both are set).
	recipient := in.ReceiverPhone
	byAddress := in.ReceiverAddress != ""
	if byAddress {
		recipient = in.ReceiverAddress
	}

	// DIRECT_CRYPTO:{TOKEN}:{AMOUNT} bypasses the FX quote step for same-token
	// transfers. USDC ≈ USD 1:1 so no rate conversion is required.
	directQuoteID := fmt.Sprintf("DIRECT_CRYPTO:%s:%s", in.Token, in.Amount)

	ref := fmt.Sprintf("CRYPTO-%s", uuid.New().String()[:8])
	txResp, err := s.caasClient.Transfer(ctx, caas.TransferRequest{
		IdempotencyKey:  ref,
		SenderPhone:     in.SenderPhone,
		RecipientPhone:  recipient, // E.164 phone OR 0x wallet address (auto-detected)
		LocalFiatAmount: in.Amount, // USDC amount (USD ≈ USDC, no conversion)
		QuoteID:         directQuoteID,
		TargetToken:     caas.Token(in.Token),
	})
	if err != nil {
		return nil, fmt.Errorf("caas transfer: %w", err)
	}

	// Resolve receiver's local user ID if they're on DigitalFX — by wallet address
	// when addressed that way, otherwise by phone.
	var receiverUserID *uuid.UUID
	if byAddress {
		if wallet, werr := q.GetCaasWalletByAddress(ctx, in.ReceiverAddress); werr == nil {
			receiverUserID = &wallet.UserID
		}
	} else if receiverUser, rerr := q.GetUserByPhoneAny(ctx, phoneCandidates(in.ReceiverPhone)); rerr == nil {
		receiverUserID = &receiverUser.ID
	}

	// Store the recipient in the column that matches how it was addressed.
	var receiverPhone, receiverAddress *string
	if byAddress {
		receiverAddress = &in.ReceiverAddress
	} else {
		receiverPhone = &in.ReceiverPhone
	}

	transferID := txResp.TransferID
	localCurrency := "USD"
	tx, err := q.CreateCryptoTransaction(ctx, db.CreateCryptoTransactionParams{
		Reference:       ref,
		SenderUserID:    in.SenderUserID,
		ReceiverPhone:   receiverPhone,
		ReceiverAddress: receiverAddress,
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

func (s *CaaSService) ListTransactions(ctx context.Context, userID uuid.UUID, page, perPage int32) ([]db.CryptoTransaction, error) {
	q := db.New(s.pool)
	return q.ListCryptoTransactionsByUser(ctx, db.ListCryptoTransactionsByUserParams{
		SenderUserID:   userID,
		ReceiverUserID: &userID,
		Limit:          perPage,
		Offset:         (page - 1) * perPage,
	})
}

// ─── Swap (USDC ⇄ USDT) ───────────────────────────────────────────────────────

type SwapCryptoInput struct {
	UserID   uuid.UUID
	Phone    string // authenticated user's SCW phone (E.164)
	TokenIn  string // USDC | USDT — token to spend
	TokenOut string // USDC | USDT — token to receive (must differ from TokenIn)
	Amount   string // input token amount, decimal string e.g. "100.00"
}

// Swap converts one stablecoin to the other on the user's SCW via the CaaS
// /v1/swaps endpoint. CaaS settles asynchronously on-chain, so we record the
// swap as `pending`, submit it, store the returned swap_id, and finalise the
// record from the swap.settled / swap.failed webhook (or a status poll).
func (s *CaaSService) Swap(ctx context.Context, in SwapCryptoInput) (*db.CaasSwap, error) {
	in.TokenIn = strings.ToUpper(strings.TrimSpace(in.TokenIn))
	in.TokenOut = strings.ToUpper(strings.TrimSpace(in.TokenOut))
	in.Amount = strings.TrimSpace(in.Amount)
	if !isStableToken(in.TokenIn) || !isStableToken(in.TokenOut) {
		return nil, ErrUnsupportedToken
	}
	if in.TokenIn == in.TokenOut {
		return nil, ErrSameSwapToken
	}
	if in.Amount == "" || in.Amount == "0" {
		return nil, ErrSwapInvalidAmount
	}

	q := db.New(s.pool)
	ref := fmt.Sprintf("SWAP-%s", uuid.New().String()[:8])

	// Record intent first so a webhook racing the HTTP response still finds a row.
	swap, err := q.CreateCaasSwap(ctx, db.CreateCaasSwapParams{
		Reference:      ref,
		UserID:         in.UserID,
		TokenIn:        in.TokenIn,
		TokenOut:       in.TokenOut,
		AmountIn:       in.Amount,
		Status:         "pending",
		IdempotencyKey: &ref,
	})
	if err != nil {
		return nil, fmt.Errorf("record swap: %w", err)
	}

	resp, err := s.caasClient.Swap(ctx, caas.SwapRequest{
		SenderPhone:    in.Phone,
		TokenIn:        caas.Token(in.TokenIn),
		TokenOut:       caas.Token(in.TokenOut),
		Amount:         in.Amount,
		IdempotencyKey: ref,
	})
	if err != nil {
		// Mark the local record failed so it doesn't sit pending forever.
		if _, uerr := q.UpdateCaasSwapResult(ctx, db.UpdateCaasSwapResultParams{
			ID: swap.ID, Status: "failed", AmountOut: nil, TxHash: nil,
		}); uerr != nil {
			s.logger.Warn("mark swap failed after caas error", zap.String("reference", ref), zap.Error(uerr))
		}
		return nil, fmt.Errorf("caas swap: %w", err)
	}

	status := resp.Status
	if status == "" {
		status = "processing"
	}
	caasID := resp.SwapID
	updated, err := q.UpdateCaasSwapCaasID(ctx, db.UpdateCaasSwapCaasIDParams{
		ID:         swap.ID,
		CaasSwapID: &caasID,
		Status:     status,
	})
	if err != nil {
		// Non-fatal: the swap is submitted; return the pre-update record.
		s.logger.Warn("update swap caas id", zap.String("reference", ref), zap.Error(err))
		return &swap, nil
	}
	return &updated, nil
}

// ListSwaps returns a user's swap history, most recent first.
func (s *CaaSService) ListSwaps(ctx context.Context, userID uuid.UUID, page, perPage int32) ([]db.CaasSwap, error) {
	if perPage <= 0 {
		perPage = 20
	}
	if page <= 0 {
		page = 1
	}
	q := db.New(s.pool)
	return q.ListCaasSwapsByUser(ctx, db.ListCaasSwapsByUserParams{
		UserID: userID,
		Limit:  perPage,
		Offset: (page - 1) * perPage,
	})
}

// GetSwap returns a single swap by its local id, scoped to the owner.
func (s *CaaSService) GetSwap(ctx context.Context, userID, swapID uuid.UUID) (*db.CaasSwap, error) {
	q := db.New(s.pool)
	swap, err := q.GetCaasSwapByID(ctx, swapID)
	if err != nil {
		return nil, ErrSwapNotFound
	}
	if swap.UserID != userID {
		return nil, ErrSwapNotFound
	}
	return &swap, nil
}

// RefreshSwap polls CaaS for the latest status of a non-terminal swap and
// persists any change. Safe to call on already-terminal swaps (no-op).
func (s *CaaSService) RefreshSwap(ctx context.Context, userID, swapID uuid.UUID) (*db.CaasSwap, error) {
	swap, err := s.GetSwap(ctx, userID, swapID)
	if err != nil {
		return nil, err
	}
	if isTerminalSwapStatus(swap.Status) || swap.CaasSwapID == nil || *swap.CaasSwapID == "" {
		return swap, nil
	}
	st, err := s.caasClient.GetSwapStatus(ctx, *swap.CaasSwapID)
	if err != nil {
		return swap, nil // best-effort — return what we have
	}
	return s.applySwapStatus(ctx, swap.ID, st.Status, st.AmountOut, st.TxHash)
}

// applySwapStatus normalises a CaaS status and persists the swap result. Shared
// by the poll path (RefreshSwap) and the swap.settled / swap.failed webhook.
func (s *CaaSService) applySwapStatus(ctx context.Context, id uuid.UUID, rawStatus, amountOut, txHash string) (*db.CaasSwap, error) {
	status := normaliseSwapStatus(rawStatus)
	var amtPtr, hashPtr *string
	if amountOut != "" {
		amtPtr = &amountOut
	}
	if txHash != "" {
		hashPtr = &txHash
	}
	updated, err := db.New(s.pool).UpdateCaasSwapResult(ctx, db.UpdateCaasSwapResultParams{
		ID: id, Status: status, AmountOut: amtPtr, TxHash: hashPtr,
	})
	if err != nil {
		return nil, fmt.Errorf("update swap result: %w", err)
	}
	return &updated, nil
}

func isTerminalSwapStatus(s string) bool { return s == "settled" || s == "failed" }

// normaliseSwapStatus maps CaaS status strings onto our canonical set.
func normaliseSwapStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "settled", "completed", "success", "succeeded", "confirmed":
		return "settled"
	case "failed", "error", "reverted":
		return "failed"
	case "", "pending", "processing", "queued", "submitted":
		return "processing"
	default:
		return strings.ToLower(s)
	}
}

// ─── Webhook appliers (called by the CaaS webhook receiver) ───────────────────

// WalletOwner resolves a CaaS smart-contract wallet address (0x…) to the local
// user who owns it. Used by the transfer.received webhook, which identifies the
// recipient by wallet address rather than phone.
func (s *CaaSService) WalletOwner(ctx context.Context, address string) (uuid.UUID, bool) {
	if address == "" {
		return uuid.Nil, false
	}
	w, err := db.New(s.pool).GetCaasWalletByAddress(ctx, address)
	if err != nil {
		return uuid.Nil, false
	}
	return w.UserID, true
}

// ApplySwapWebhook finalises a swap from a swap.settled / swap.failed webhook.
// It resolves the local swap by the CaaS swap_id and persists the terminal
// status, output amount and on-chain tx hash. Returns the updated swap (whose
// UserID the caller uses to notify the owner).
func (s *CaaSService) ApplySwapWebhook(ctx context.Context, caasSwapID, status, amountOut, txHash string) (*db.CaasSwap, error) {
	if caasSwapID == "" {
		return nil, ErrSwapNotFound
	}
	swap, err := db.New(s.pool).GetCaasSwapByCaasID(ctx, &caasSwapID)
	if err != nil {
		return nil, ErrSwapNotFound
	}
	return s.applySwapStatus(ctx, swap.ID, status, amountOut, txHash)
}

// ApplyTransferWebhook finalises an outbound CaaS transfer from a
// transfer.settled / transfer.failed webhook. It resolves the local crypto
// transaction by the CaaS transfer_id (falling back to the reference /
// idempotency key) and updates its status and on-chain tx hash. Returns the
// updated transaction (whose SenderUserID the caller uses to notify the sender).
func (s *CaaSService) ApplyTransferWebhook(ctx context.Context, transferID, reference, status, txHash string) (*db.CryptoTransaction, error) {
	q := db.New(s.pool)

	var tx db.CryptoTransaction
	var err error
	switch {
	case transferID != "":
		tx, err = q.GetCryptoTransactionByCaasTransferID(ctx, &transferID)
	default:
		err = ErrTransferNotFound
	}
	// Fall back to the reference / idempotency key when the transfer_id lookup misses.
	if err != nil && reference != "" {
		tx, err = q.GetCryptoTransactionByIdempotencyKey(ctx, &reference)
	}
	if err != nil {
		return nil, ErrTransferNotFound
	}

	newStatus := normaliseTransferStatus(status)
	// Keep the existing on-chain hash if the webhook didn't carry one.
	hashPtr := tx.TxHash
	if txHash != "" {
		hashPtr = &txHash
	}
	caasID := tx.CaasTransferID
	if transferID != "" {
		caasID = &transferID
	}
	updated, err := q.UpdateCryptoTransactionCaasResult(ctx, db.UpdateCryptoTransactionCaasResultParams{
		ID: tx.ID, Status: newStatus, TxHash: hashPtr, CaasTransferID: caasID,
	})
	if err != nil {
		return nil, fmt.Errorf("update transfer status: %w", err)
	}
	return &updated, nil
}

// normaliseTransferStatus maps CaaS transfer status strings onto our canonical set.
func normaliseTransferStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "settled", "completed", "success", "succeeded", "confirmed":
		return "completed"
	case "failed", "error", "reverted":
		return "failed"
	case "", "pending", "processing", "queued", "submitted":
		return "processing"
	default:
		return strings.ToLower(s)
	}
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
func (s *CaaSService) Withdraw(ctx context.Context, in WithdrawCryptoInput) (*db.CaasWithdrawal, error) {
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
func (s *CaaSService) UpdatePhone(ctx context.Context, userID uuid.UUID, oldPhone, newPhone string) error {
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
