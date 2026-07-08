package services

// WithdrawalService handles fiat withdrawals from Nilos-backed USD/EUR/GBP accounts.
//
// Three withdrawal channels:
//
//  1. Mobile Money (XAF/XOF) — Raenest-style:
//     Source: user's Nilos USD/EUR/GBP account (local balance)
//     Destination: MTN/Orange/Wave/Moov mobile money
//     FX: business-set rate (e.g. USD→XAF 595) — business earns the spread
//     Processor: HUB2 Disburse; async webhook finalises balance
//
//  2. Bank (SEPA/SWIFT/FPS/CEMAC/UEMOA):
//     Source: user's Nilos account
//     Destination: external IBAN / account number
//     FX: Nilos quote (cross-currency) or none (same currency)
//     Processor: Nilos payout transfer; webhook finalises
//
//  3. CaaS off-ramp (USDC → mobile money) is handled by CryptoService.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/hub2"
	"github.com/rachfinance/digitalfx/internal/clients/nilos"
	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

// Destination type constants.
const (
	DestMobileMoney = "mobile_money"
	DestBankSEPA    = "bank_sepa"
	DestBankSWIFT   = "bank_swift"
	DestBankFPS     = "bank_fps"
	DestBankCEMAC   = "bank_cemac"
	DestBankUEMOA   = "bank_uemoa"
)

var (
	ErrRateNotConfigured     = errors.New("withdrawal: FX rate not configured for this currency pair")
	ErrInsufficientFiatFunds = errors.New("withdrawal: insufficient fiat balance")
	ErrWithdrawalNotFound    = errors.New("withdrawal: not found")
	ErrBeneficiaryNotFound   = errors.New("withdrawal: beneficiary not found")
	ErrLimitExceeded         = errors.New("withdrawal: daily limit of $10,000 USD exceeded")
)

type WithdrawalService struct {
	pool        *pgxpool.Pool
	hub2Client  *hub2.Client
	nilosClient *nilos.Client
	notif       *NotificationService
	logger      *zap.Logger
}

func NewWithdrawalService(
	pool *pgxpool.Pool,
	hub2Client *hub2.Client,
	nilosClient *nilos.Client,
	notif *NotificationService,
	logger *zap.Logger,
) *WithdrawalService {
	return &WithdrawalService{
		pool:        pool,
		hub2Client:  hub2Client,
		nilosClient: nilosClient,
		notif:       notif,
		logger:      logger,
	}
}

// ─── Quote ────────────────────────────────────────────────────────────────────

type WithdrawalQuoteRequest struct {
	SourceCurrency      string  `json:"source_currency"`
	SourceAmount        float64 `json:"source_amount"`
	DestinationType     string  `json:"destination_type"`
	DestinationCurrency string  `json:"destination_currency"`
}

type WithdrawalQuoteResponse struct {
	SourceCurrency      string   `json:"source_currency"`
	SourceAmount        float64  `json:"source_amount"`
	Fee                 float64  `json:"fee"`
	FeeCurrency         string   `json:"fee_currency"`
	NetSourceAmount     float64  `json:"net_source_amount"`
	FxRate              *float64 `json:"fx_rate,omitempty"`
	DestinationCurrency string   `json:"destination_currency"`
	DestinationAmount   float64  `json:"destination_amount"`
}

// Quote previews the exact fee and destination amount before the user submits.
func (s *WithdrawalService) Quote(ctx context.Context, req WithdrawalQuoteRequest) (*WithdrawalQuoteResponse, error) {
	q := db.New(s.pool)
	resp := &WithdrawalQuoteResponse{
		SourceCurrency:      req.SourceCurrency,
		SourceAmount:        req.SourceAmount,
		FeeCurrency:         req.SourceCurrency,
		DestinationCurrency: req.DestinationCurrency,
	}

	if req.SourceCurrency == req.DestinationCurrency {
		// Same-currency bank transfer — default 0.5% service fee unless overridden.
		feePercent := 0.005
		rate, err := q.GetBusinessFxRate(ctx, db.GetBusinessFxRateParams{
			SourceCurrency: req.SourceCurrency,
			TargetCurrency: req.DestinationCurrency,
		})
		if err == nil && rate.IsActive {
			if fp := pgNumericF64(rate.FeePercent); fp > 0 {
				feePercent = fp
			}
		}
		fee := wdRound(req.SourceAmount * feePercent)
		net := wdRound(req.SourceAmount - fee)
		resp.Fee = fee
		resp.NetSourceAmount = net
		resp.DestinationAmount = net
		return resp, nil
	}

	// Cross-currency: require an admin-configured business FX rate.
	rate, err := q.GetBusinessFxRate(ctx, db.GetBusinessFxRateParams{
		SourceCurrency: req.SourceCurrency,
		TargetCurrency: req.DestinationCurrency,
	})
	if err != nil || !rate.IsActive {
		return nil, ErrRateNotConfigured
	}

	rateVal := pgNumericF64(rate.Rate)
	feePercent := pgNumericF64(rate.FeePercent)
	flatFee := pgNumericF64(rate.FlatFee)
	if rateVal == 0 {
		return nil, fmt.Errorf("invalid rate value for %s→%s", req.SourceCurrency, req.DestinationCurrency)
	}

	fee := wdRound(req.SourceAmount*feePercent + flatFee)
	net := wdRound(req.SourceAmount - fee)
	if net <= 0 {
		return nil, fmt.Errorf("withdrawal amount too small to cover fees")
	}
	destAmount := wdRound(net * rateVal)

	resp.Fee = fee
	resp.NetSourceAmount = net
	resp.FxRate = &rateVal
	resp.DestinationAmount = destAmount
	return resp, nil
}

// ─── Initiate ─────────────────────────────────────────────────────────────────

type InitiateWithdrawalRequest struct {
	SourceCurrency      string  `json:"source_currency"`
	SourceAmount        float64 `json:"source_amount"`
	DestinationType     string  `json:"destination_type"`
	DestinationCurrency string  `json:"destination_currency"`
	DestinationCountry  string  `json:"destination_country"`
	RecipientName       string  `json:"recipient_name"`
	// Mobile money
	PhoneNumber string `json:"phone_number"`
	Operator    string `json:"operator"`
	// Bank
	BankName      string `json:"bank_name"`
	AccountNumber string `json:"account_number"`
	IBAN          string `json:"iban"`
	SwiftCode     string `json:"swift_code"`
	SortCode      string `json:"sort_code"`
	RoutingNumber string `json:"routing_number"`
	// Link to a saved beneficiary (optional, speeds up repeat withdrawals)
	BeneficiaryID *uuid.UUID `json:"beneficiary_id"`
}

// Initiate validates, holds funds, submits to the external processor, and
// returns the withdrawal record. Status is "pending" → "processing" on return.
func (s *WithdrawalService) Initiate(ctx context.Context, userID uuid.UUID, req InitiateWithdrawalRequest) (*db.FiatWithdrawal, error) {
	q := db.New(s.pool)

	// Validate user has enough available balance.
	acct, err := q.GetAccountWithNilosByUserAndCurrency(ctx, userID, req.SourceCurrency)
	if err != nil {
		return nil, ErrAccountNotFound
	}
	avail, err := strconv.ParseFloat(acct.AvailableBalance, 64)
	if err != nil || avail < req.SourceAmount {
		return nil, ErrInsufficientFiatFunds
	}

	// Enforce the $10,000 rolling 24h withdrawal limit across fiat and crypto.
	incomingUSD := req.SourceAmount
	switch req.SourceCurrency {
	case "EUR":
		incomingUSD *= 1.08
	case "GBP":
		incomingUSD *= 1.27
	}
	if err := s.checkDailyLimit(ctx, userID, incomingUSD); err != nil {
		return nil, err
	}

	// Calculate fee and destination amount.
	quote, err := s.Quote(ctx, WithdrawalQuoteRequest{
		SourceCurrency:      req.SourceCurrency,
		SourceAmount:        req.SourceAmount,
		DestinationType:     req.DestinationType,
		DestinationCurrency: req.DestinationCurrency,
	})
	if err != nil {
		return nil, err
	}

	// Hold the funds (reduces available balance, not settled balance).
	if err := q.DeductAvailableBalance(ctx, db.DeductAvailableBalanceParams{
		ID:     acct.ID,
		Amount: wdFmt(req.SourceAmount),
	}); err != nil {
		return nil, fmt.Errorf("hold funds: %w", err)
	}

	// Create withdrawal record (status = "pending").
	ref := fmt.Sprintf("WD-%s-%d", userID.String()[:8], time.Now().UnixMilli())
	var fxRateStr *string
	if quote.FxRate != nil {
		v := wdFmt(*quote.FxRate)
		fxRateStr = &v
	}
	w, err := q.CreateFiatWithdrawal(ctx, db.CreateFiatWithdrawalParams{
		UserID:              userID,
		SourceCurrency:      req.SourceCurrency,
		SourceAmount:        wdFmt(req.SourceAmount),
		Fee:                 wdFmt(quote.Fee),
		FeeCurrency:         req.SourceCurrency,
		FxRate:              fxRateStr,
		DestinationType:     req.DestinationType,
		DestinationCurrency: req.DestinationCurrency,
		DestinationAmount:   wdFmt(quote.DestinationAmount),
		DestinationCountry:  req.DestinationCountry,
		RecipientName:       req.RecipientName,
		PhoneNumber:         strPtr(req.PhoneNumber),
		Operator:            strPtr(req.Operator),
		BankName:            strPtr(req.BankName),
		AccountNumber:       strPtr(req.AccountNumber),
		IBAN:                strPtr(req.IBAN),
		SwiftCode:           strPtr(req.SwiftCode),
		SortCode:            strPtr(req.SortCode),
		RoutingNumber:       strPtr(req.RoutingNumber),
		Reference:           ref,
		BeneficiaryID:       req.BeneficiaryID,
	})
	if err != nil {
		// Release hold.
		_ = q.RestoreAvailableBalance(ctx, db.RestoreAvailableBalanceParams{
			ID: acct.ID, Amount: wdFmt(req.SourceAmount),
		})
		return nil, fmt.Errorf("create withdrawal record: %w", err)
	}

	// Submit to external processor.
	var procErr error
	switch req.DestinationType {
	case DestMobileMoney:
		procErr = s.processMobileMoney(ctx, q, acct.ID, &w, req, quote)
	default:
		procErr = s.processBankTransfer(ctx, q, acct, &w, req, quote)
	}

	if procErr != nil {
		_ = q.RestoreAvailableBalance(ctx, db.RestoreAvailableBalanceParams{
			ID: acct.ID, Amount: wdFmt(req.SourceAmount),
		})
		reason := procErr.Error()
		updated, _ := q.UpdateFiatWithdrawalStatus(ctx, db.UpdateFiatWithdrawalStatusParams{
			ID: w.ID, Status: "failed", FailureReason: &reason,
		})
		return &updated, fmt.Errorf("process withdrawal: %w", procErr)
	}

	s.notif.Create(ctx, CreateNotificationInput{
		UserID: userID,
		Type:   NotifWithdrawal,
		Title:  "Withdrawal Processing",
		Body: fmt.Sprintf(
			"Your %s %.2f withdrawal (%.2f %s) is being processed.",
			req.SourceCurrency, req.SourceAmount,
			quote.DestinationAmount, req.DestinationCurrency,
		),
		Metadata: map[string]string{
			"reference":            w.Reference,
			"source_currency":      req.SourceCurrency,
			"destination_currency": req.DestinationCurrency,
		},
	})

	return &w, nil
}

// processMobileMoney disburses local currency to a mobile money number via HUB2.
func (s *WithdrawalService) processMobileMoney(
	ctx context.Context,
	q *db.Queries,
	_ uuid.UUID,
	w *db.FiatWithdrawal,
	req InitiateWithdrawalRequest,
	quote *WithdrawalQuoteResponse,
) error {
	resp, err := s.hub2Client.Disburse(ctx, hub2.DisburseRequest{
		Amount:      quote.DestinationAmount,
		Currency:    req.DestinationCurrency,
		Phone:       req.PhoneNumber,
		Operator:    req.Operator,
		Description: fmt.Sprintf("DigitalFX withdrawal %s", w.Reference),
	})
	if err != nil {
		return fmt.Errorf("hub2 disburse: %w", err)
	}

	updated, err := q.UpdateFiatWithdrawalHub2Ref(ctx, db.UpdateFiatWithdrawalHub2RefParams{
		ID:            w.ID,
		Hub2Reference: resp.Reference,
		Status:        "processing",
	})
	if err != nil {
		s.logger.Error("update withdrawal hub2_ref", zap.Error(err))
	} else {
		*w = updated
	}

	s.logger.Info("mobile money disburse initiated",
		zap.String("wd_ref", w.Reference),
		zap.String("hub2_ref", resp.Reference),
		zap.Float64("amount", quote.DestinationAmount),
		zap.String("currency", req.DestinationCurrency),
		zap.String("phone", req.PhoneNumber),
	)
	return nil
}

// processBankTransfer creates a Nilos recipient (if needed) and payout transfer.
func (s *WithdrawalService) processBankTransfer(
	ctx context.Context,
	q *db.Queries,
	acct db.AccountWithNilos,
	w *db.FiatWithdrawal,
	req InitiateWithdrawalRequest,
	quote *WithdrawalQuoteResponse,
) error {
	if acct.NilosAccountID == nil || *acct.NilosAccountID == "" {
		return fmt.Errorf("no Nilos account provisioned for %s", req.SourceCurrency)
	}

	// Resolve or create the Nilos recipient.
	nilosRecipientID := ""
	if req.BeneficiaryID != nil {
		ben, err := q.GetBeneficiaryByID(ctx, db.GetBeneficiaryByIDParams{
			ID: *req.BeneficiaryID, UserID: w.UserID,
		})
		if err == nil && ben.NilosRecipientID != nil {
			nilosRecipientID = *ben.NilosRecipientID
		}
	}
	if nilosRecipientID == "" {
		recipientInfo, rail := buildRecipientInfo(req)
		rec, err := s.nilosClient.CreateRecipient(ctx, nilos.CreateRecipientRequest{
			Name:          req.RecipientName,
			Rail:          rail,
			RecipientInfo: recipientInfo,
		})
		if err != nil {
			return fmt.Errorf("nilos create recipient: %w", err)
		}
		nilosRecipientID = rec.ID
		if req.BeneficiaryID != nil {
			_ = q.UpdateBeneficiaryNilosRecipient(ctx, *req.BeneficiaryID, nilosRecipientID)
		}
	}

	// Build and optionally quote-lock the payout transfer.
	payoutReq := nilos.CreatePayoutTransferRequest{
		AccountID:      *acct.NilosAccountID,
		SourceCurrency: req.SourceCurrency,
		RecipientID:    nilosRecipientID,
		TargetCurrency: req.DestinationCurrency,
		Amount:         quote.NetSourceAmount,
		Side:           nilos.SideSell,
		Reference:      w.Reference,
		Reason:         "GOODS_SERVICES",
	}
	if req.SourceCurrency != req.DestinationCurrency {
		_, srcRail := buildRecipientInfo(InitiateWithdrawalRequest{
			DestinationType: destTypeToSrcRail(req.SourceCurrency),
		})
		_, dstRail := buildRecipientInfo(req)
		nilosQuote, err := s.nilosClient.CreateQuote(ctx, nilos.CreateQuoteRequest{
			SourceCurrency: req.SourceCurrency,
			SourceRail:     srcRail,
			TargetCurrency: req.DestinationCurrency,
			TargetRail:     dstRail,
			Amount:         quote.NetSourceAmount,
			Side:           nilos.SideSell,
		})
		if err != nil {
			return fmt.Errorf("nilos quote: %w", err)
		}
		payoutReq.QuoteID = nilosQuote.ID
	}

	payout, err := s.nilosClient.CreatePayoutTransfer(ctx, payoutReq)
	if err != nil {
		return fmt.Errorf("nilos payout transfer: %w", err)
	}

	updated, err := q.UpdateFiatWithdrawalNilosRef(ctx, db.UpdateFiatWithdrawalNilosRefParams{
		ID:               w.ID,
		NilosPayoutID:    payout.ID,
		NilosRecipientID: nilosRecipientID,
		Status:           "processing",
	})
	if err != nil {
		s.logger.Error("update withdrawal nilos_ref", zap.Error(err))
	} else {
		*w = updated
	}

	s.logger.Info("nilos bank transfer initiated",
		zap.String("wd_ref", w.Reference),
		zap.String("nilos_payout_id", payout.ID),
		zap.String("source", req.SourceCurrency),
		zap.String("target", req.DestinationCurrency),
	)
	return nil
}

// ─── HUB2 disbursement webhook ────────────────────────────────────────────────

// HandleDisbursementResult is called by HUB2Service when a disbursement status
// update arrives. It finalises the withdrawal and notifies the user.
func (s *WithdrawalService) HandleDisbursementResult(ctx context.Context, hub2Ref, hub2Status string) {
	q := db.New(s.pool)

	w, err := q.GetFiatWithdrawalByHub2Ref(ctx, hub2Ref)
	if err != nil {
		return // not a fiat withdrawal disbursement
	}

	switch hub2Status {
	case "SUCCESSFUL":
		// Settled: also deduct the settled balance (available was already held).
		acct, err := q.GetAccountWithNilosByUserAndCurrency(ctx, w.UserID, w.SourceCurrency)
		if err == nil {
			_ = q.DeductBalance(ctx, db.DeductBalanceParams{
				ID:     acct.ID,
				Amount: wdFmt(pgNumericF64(w.SourceAmount)),
			})
		}
		_, _ = q.UpdateFiatWithdrawalStatus(ctx, db.UpdateFiatWithdrawalStatusParams{
			ID: w.ID, Status: "completed",
		})
		s.notif.Create(ctx, CreateNotificationInput{
			UserID: w.UserID,
			Type:   NotifWithdrawal,
			Title:  "Withdrawal Completed",
			Body: fmt.Sprintf(
				"%.2f %s has been sent to %s.",
				pgNumericF64(w.DestinationAmount), w.DestinationCurrency,
				deref(w.PhoneNumber),
			),
			Metadata: map[string]string{"reference": w.Reference},
		})

	case "FAILED", "CANCELLED":
		// Restore the held amount back to available balance.
		acct, err := q.GetAccountWithNilosByUserAndCurrency(ctx, w.UserID, w.SourceCurrency)
		if err == nil {
			_ = q.RestoreAvailableBalance(ctx, db.RestoreAvailableBalanceParams{
				ID:     acct.ID,
				Amount: wdFmt(pgNumericF64(w.SourceAmount)),
			})
		}
		reason := "HUB2 disbursement " + hub2Status
		_, _ = q.UpdateFiatWithdrawalStatus(ctx, db.UpdateFiatWithdrawalStatusParams{
			ID: w.ID, Status: "failed", FailureReason: &reason,
		})
		s.notif.Create(ctx, CreateNotificationInput{
			UserID: w.UserID,
			Type:   NotifWithdrawal,
			Title:  "Withdrawal Failed",
			Body: fmt.Sprintf(
				"Your withdrawal of %.2f %s failed. Funds have been returned to your account.",
				pgNumericF64(w.SourceAmount), w.SourceCurrency,
			),
			Metadata: map[string]string{"reference": w.Reference},
		})
	}
}

// ─── Queries ─────────────────────────────────────────────────────────────────

type WithdrawalListResult struct {
	Withdrawals []db.FiatWithdrawal `json:"withdrawals"`
	Total       int64               `json:"total"`
	Page        int32               `json:"page"`
	PerPage     int32               `json:"per_page"`
}

func (s *WithdrawalService) Get(ctx context.Context, id, userID uuid.UUID) (*db.FiatWithdrawal, error) {
	w, err := db.New(s.pool).GetFiatWithdrawalByID(ctx, db.GetFiatWithdrawalByIDParams{ID: id, UserID: userID})
	if err != nil {
		return nil, ErrWithdrawalNotFound
	}
	return &w, nil
}

func (s *WithdrawalService) List(ctx context.Context, userID uuid.UUID, page, perPage int32) (*WithdrawalListResult, error) {
	q := db.New(s.pool)
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 50 {
		perPage = 20
	}
	rows, err := q.ListFiatWithdrawals(ctx, db.ListFiatWithdrawalsParams{
		UserID: userID, Limit: perPage, Offset: (page - 1) * perPage,
	})
	if err != nil {
		return nil, err
	}
	total, _ := q.CountFiatWithdrawals(ctx, userID)
	return &WithdrawalListResult{Withdrawals: rows, Total: total, Page: page, PerPage: perPage}, nil
}

// ─── Beneficiaries ────────────────────────────────────────────────────────────

type SaveBeneficiaryRequest struct {
	Label               string `json:"label"`
	Type                string `json:"type"`
	DestinationCurrency string `json:"destination_currency"`
	Country             string `json:"country"`
	PhoneNumber         string `json:"phone_number"`
	Operator            string `json:"operator"`
	BankName            string `json:"bank_name"`
	AccountNumber       string `json:"account_number"`
	IBAN                string `json:"iban"`
	SwiftCode           string `json:"swift_code"`
	SortCode            string `json:"sort_code"`
	RoutingNumber       string `json:"routing_number"`
}

func (s *WithdrawalService) ListBeneficiaries(ctx context.Context, userID uuid.UUID) ([]db.Beneficiary, error) {
	return db.New(s.pool).ListBeneficiaries(ctx, userID)
}

func (s *WithdrawalService) SaveBeneficiary(ctx context.Context, userID uuid.UUID, req SaveBeneficiaryRequest) (*db.Beneficiary, error) {
	b, err := db.New(s.pool).CreateBeneficiary(ctx, db.CreateBeneficiaryParams{
		UserID:              userID,
		Label:               req.Label,
		Type:                req.Type,
		DestinationCurrency: req.DestinationCurrency,
		Country:             req.Country,
		PhoneNumber:         strPtr(req.PhoneNumber),
		Operator:            strPtr(req.Operator),
		BankName:            strPtr(req.BankName),
		AccountNumber:       strPtr(req.AccountNumber),
		IBAN:                strPtr(req.IBAN),
		SwiftCode:           strPtr(req.SwiftCode),
		SortCode:            strPtr(req.SortCode),
		RoutingNumber:       strPtr(req.RoutingNumber),
	})
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *WithdrawalService) DeleteBeneficiary(ctx context.Context, id, userID uuid.UUID) error {
	return db.New(s.pool).DeleteBeneficiary(ctx, id, userID)
}

// ─── Admin: FX rate management ───────────────────────────────────────────────

type SetRateRequest struct {
	SourceCurrency string  `json:"source_currency"` // e.g. "USD"
	TargetCurrency string  `json:"target_currency"` // e.g. "XAF"
	Rate           float64 `json:"rate"`            // e.g. 595.0 (1 USD = 595 XAF)
	FeePercent     float64 `json:"fee_percent"`     // e.g. 0.01 = 1%
	FlatFee        float64 `json:"flat_fee"`        // e.g. 0.50 flat fee in source currency
}

func (s *WithdrawalService) SetRate(ctx context.Context, adminID uuid.UUID, req SetRateRequest) (*db.BusinessFxRate, error) {
	r, err := db.New(s.pool).UpsertBusinessFxRate(ctx, db.UpsertBusinessFxRateParams{
		SourceCurrency: req.SourceCurrency,
		TargetCurrency: req.TargetCurrency,
		Rate:           wdFmt(req.Rate),
		FeePercent:     wdFmt(req.FeePercent),
		FlatFee:        wdFmt(req.FlatFee),
		SetBy:          &adminID,
	})
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *WithdrawalService) ListRates(ctx context.Context) ([]db.BusinessFxRate, error) {
	return db.New(s.pool).ListBusinessFxRates(ctx)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func buildRecipientInfo(req InitiateWithdrawalRequest) (nilos.RecipientInfo, string) {
	info := nilos.RecipientInfo{
		RecipientName:    req.RecipientName,
		RecipientCountry: req.DestinationCountry,
		AccountType:      "personal",
	}
	switch req.DestinationType {
	case DestBankSEPA:
		info.IBAN = req.IBAN
		return info, nilos.RailSEPA
	case DestBankSWIFT:
		info.SwiftCode = req.SwiftCode
		info.AccountNumber = req.AccountNumber
		return info, nilos.RailSWIFT
	case DestBankFPS:
		info.SortCode = req.SortCode
		info.AccountNumber = req.AccountNumber
		return info, nilos.RailFPS
	case DestBankCEMAC:
		info.IBAN = req.IBAN
		return info, nilos.RailCEMACBank
	case DestBankUEMOA:
		info.AccountNumber = req.AccountNumber
		info.BankCode = req.RoutingNumber
		return info, nilos.RailUEMOA
	default:
		return info, nilos.RailSEPA
	}
}

// destTypeToSrcRail returns the Nilos rail for the source account based on currency.
func destTypeToSrcRail(sourceCurrency string) string {
	switch sourceCurrency {
	case "EUR":
		return DestBankSEPA
	case "GBP":
		return DestBankFPS
	default:
		return DestBankSWIFT
	}
}

// pgNumericF64 extracts a float64 from a pgtype.Numeric, returning 0 on failure.
func pgNumericF64(n pgtype.Numeric) float64 {
	f8, err := n.Float64Value()
	if err != nil || !f8.Valid {
		return 0
	}
	return f8.Float64
}

// wdFmt serialises a float64 as an 8-decimal-place string for DB storage.
func wdFmt(f float64) string {
	return strconv.FormatFloat(f, 'f', 8, 64)
}

// wdRound normalises a float64 to 8 decimal places to avoid floating-point drift.
func wdRound(f float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(f, 'f', 8, 64), 64)
	return v
}

// strPtr returns nil for an empty string, otherwise a pointer to the string.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// deref safely dereferences a *string.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// checkDailyLimit aggregates the user's total fiat and crypto withdrawals in the last 24 hours.
// Returns an error if the daily limit of $10,000 USD is exceeded.
func (s *WithdrawalService) checkDailyLimit(ctx context.Context, userID uuid.UUID, incomingUSD float64) error {
	// Query fiat_withdrawals in last 24 hours
	rows, err := s.pool.Query(ctx,
		`SELECT source_currency, source_amount FROM fiat_withdrawals 
		 WHERE user_id = $1 AND status != 'failed' AND created_at >= NOW() - INTERVAL '24 hours'`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("query daily fiat withdrawals: %w", err)
	}
	defer rows.Close()

	var totalUSD float64
	for rows.Next() {
		var currency string
		var amount pgtype.Numeric
		if err := rows.Scan(&currency, &amount); err != nil {
			return fmt.Errorf("scan daily fiat withdrawal: %w", err)
		}
		val, _ := amount.Float64Value()
		if !val.Valid {
			continue
		}

		switch currency {
		case "EUR":
			totalUSD += val.Float64 * 1.08
		case "GBP":
			totalUSD += val.Float64 * 1.27
		default:
			totalUSD += val.Float64
		}
	}

	// Query caas_withdrawals in last 24 hours
	cRows, err := s.pool.Query(ctx,
		`SELECT token, amount FROM caas_withdrawals 
		 WHERE user_id = $1 AND status != 'failed' AND created_at >= NOW() - INTERVAL '24 hours'`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("query daily caas withdrawals: %w", err)
	}
	defer cRows.Close()

	for cRows.Next() {
		var token string
		var amount pgtype.Numeric
		if err := cRows.Scan(&token, &amount); err != nil {
			return fmt.Errorf("scan daily caas withdrawal: %w", err)
		}
		val, _ := amount.Float64Value()
		if !val.Valid {
			continue
		}
		totalUSD += val.Float64
	}

	if totalUSD+incomingUSD > 10000.0 {
		return fmt.Errorf("%w: you would exceed the $10,000 daily withdrawal limit (used $%.2f in the last 24 hours)", ErrLimitExceeded, totalUSD)
	}

	return nil
}
