package services

// hub2_service.go handles everything that happens AFTER HUB2 fires a webhook.
//
// The full 9-step Instant USD Account funding flow is:
//
//  Step 1  Customer calls GET /crypto/wallet → ERC-4337 SCW provisioned on Rach CaaS.
//  Step 2  Customer calls POST /crypto/fund → CryptoService.InitiateFunding.
//  Step 3  CryptoService calls HUB2 Collect → HUB2 sends push-to-pay to customer's phone.
//  Step 4  Customer approves on their Mobile Money app.
//  Step 5  HUB2 fires POST /webhooks/hub2 (COLLECTION SUCCESSFUL).
//  Step 6  HandleWebhook (this file) calls CaaS FundUser:
//            - Gets an FX quote: 20,000 XOF → N USDC at today's rate.
//            - Tells CaaS: "N USDC is owed to this customer; XOF is on the way."
//            - DigitalFX physically transfers the XOF to Rach CaaS's bank (Ivory Coast).
//  Step 7  Rach CaaS confirms fiat receipt → engages OTC partners for conversion.
//  Step 8  OTC partners send USDC/USDT to Rach CaaS Treasury → CaaS credits SCW.
//  Step 9  Customer calls GET /crypto/balances → balance fetched live from CaaS.
//
// NOTE: Steps 7-8 are entirely internal to Rach CaaS. DigitalFX does not poll
// for completion — the customer simply re-fetches their balance and sees the update.

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

type HUB2Service struct {
	pool        *pgxpool.Pool
	hub2Client  *hub2.Client
	caasClient  *caas.Client
	withdrawal  *WithdrawalService // nil until set via SetWithdrawalService
	logger      *zap.Logger
}

func NewHUB2Service(pool *pgxpool.Pool, hub2Client *hub2.Client, caasClient *caas.Client, logger *zap.Logger) *HUB2Service {
	return &HUB2Service{
		pool:       pool,
		hub2Client: hub2Client,
		caasClient: caasClient,
		logger:     logger,
	}
}

// SetWithdrawalService wires in the withdrawal service after both are constructed
// (avoids circular dependency during New).
func (s *HUB2Service) SetWithdrawalService(ws *WithdrawalService) {
	s.withdrawal = ws
}

// HandleWebhook processes a payment status update pushed by HUB2 (step 5 → step 6).
//
// COLLECTION + SUCCESSFUL:
//   - Gets an FX quote from CaaS (locks the XOF → USDC rate for this deposit).
//   - Calls CaaS FundUser: tells CaaS the customer is owed N USDC once the XOF
//     arrives at Rach CaaS's bank account in Ivory Coast.
//   - Records a pending CryptoTransaction so the customer sees the deposit in their history.
//   - The USDC appears in the customer's balance (GET /crypto/balances) only after
//     CaaS completes the OTC conversion and credits the SCW (steps 7-8).
//
// DISBURSEMENT or non-SUCCESSFUL:
//   - Only updates the Hub2Payment status in our DB; no CaaS calls made.
func (s *HUB2Service) HandleWebhook(ctx context.Context, payload hub2.WebhookPayload) error {
	q := db.New(s.pool)

	// Reject unknown references — prevents replaying webhooks for payments we
	// never initiated, which could indicate spoofing.
	payment, err := q.GetHub2PaymentByReference(ctx, payload.Reference)
	if err != nil {
		return fmt.Errorf("hub2 reference %q not found: %w", payload.Reference, err)
	}

	// Short-circuit if we have already called CaaS FundUser for this payment.
	// Handles duplicate webhook delivery without double-crediting the SCW.
	if payload.Status == "SUCCESSFUL" && payload.Type != "DISBURSEMENT" && payment.CaasFundedAt != nil {
		s.logger.Info("hub2 webhook: caas already funded, skipping",
			zap.String("reference", payload.Reference))
		return nil
	}

	internalStatus := mapHub2Status(payload.Status)

	if payload.Status != "SUCCESSFUL" || payload.Type == "DISBURSEMENT" {
		_, err = q.UpdateHub2PaymentStatus(ctx, db.UpdateHub2PaymentStatusParams{
			ID:         payment.ID,
			Status:     internalStatus,
			Hub2Status: &payload.Status,
		})
		if err != nil {
			s.logger.Error("update hub2 payment status", zap.Error(err))
		}
		s.logger.Info("hub2 webhook recorded",
			zap.String("reference", payload.Reference),
			zap.String("status", payload.Status),
			zap.String("type", payload.Type),
		)
		// For disbursements, notify the withdrawal service so it can
		// finalise the fiat_withdrawal record and user balance.
		if payload.Type == "DISBURSEMENT" && s.withdrawal != nil {
			s.withdrawal.HandleDisbursementResult(ctx, payload.Reference, payload.Status)
		}
		return err
	}

	// ── COLLECTION SUCCESSFUL → Step 6: instruct Rach CaaS ──────────────────

	// Resolve the phone. HUB2 includes it in the webhook payload; fall back to
	// the number stored when the customer initiated the collection.
	phone := payload.Phone
	if phone == "" && payment.PhoneNumber != nil {
		phone = *payment.PhoneNumber
	}
	if phone == "" {
		return fmt.Errorf("hub2 webhook: no phone for reference %s", payload.Reference)
	}

	// Resolve user for transaction recording — not fatal if missing.
	user, _ := q.GetUserByPhone(ctx, phone)

	// Safety net: ensure SCW is provisioned before calling FundUser.
	// CaaS returns 404 if the phone has no SCW — would silently lose the deposit.
	// Under normal flow the SCW is provisioned in InitiateFunding, but this guards
	// against edge cases (e.g. direct HUB2 calls, replays from before provisioning).
	if _, err := s.caasClient.ProvisionSCW(ctx, caas.ProvisionSCWRequest{
		PhoneNumber: phone,
	}); err != nil {
		return fmt.Errorf("caas provision scw (safety net) for %s: %w", phone, err)
	}

	// Lock the FX rate: XOF → USDC. This rate is what the customer will receive
	// once CaaS confirms the fiat and executes the OTC conversion (steps 7-8).
	quote, err := s.caasClient.GetFXQuote(ctx, caas.FXQuoteRequest{
		FiatAmount:    payload.Amount,
		LocalCurrency: payload.Currency, // XOF | XAF
		TargetToken:   caas.TokenUSDC,
	})
	if err != nil {
		return fmt.Errorf("caas fx quote for %s %.2f %s: %w", phone, payload.Amount, payload.Currency, err)
	}

	s.logger.Info("fx rate locked for deposit",
		zap.String("reference", payload.Reference),
		zap.Float64("fiat_amount", payload.Amount),
		zap.String("currency", payload.Currency),
		zap.String("usdc_owed", quote.ExpectedOut),
		zap.String("rate", quote.Rate),
	)

	// Notify CaaS that this customer is owed N USDC.
	// The hub2_reference is the deposit_id — CaaS uses it for idempotency.
	// If this webhook fires twice, CaaS returns 409 and we log + continue.
	// CaaS will credit the SCW once DigitalFX's XOF transfer reaches their bank.
	fundResp, err := s.caasClient.FundUser(ctx, caas.FundUserRequest{
		DepositID:        payload.Reference,
		PhoneNumber:      phone,
		LocalFiatAmount:  fmt.Sprintf("%.2f", payload.Amount),
		StablecoinAmount: quote.ExpectedOut,
		TargetToken:      caas.TokenUSDC,
	})
	if err != nil {
		if apiErr, ok := err.(*caas.APIError); ok && apiErr.Status == 409 {
			s.logger.Info("caas fund already registered (idempotent replay)",
				zap.String("reference", payload.Reference),
			)
		} else {
			return fmt.Errorf("caas fund user %s: %w", phone, err)
		}
	} else {
		s.logger.Info("caas notified — awaiting XOF bank transfer confirmation",
			zap.String("reference", payload.Reference),
			zap.String("phone", phone),
			zap.String("usdc_owed", quote.ExpectedOut),
			zap.String("caas_status", fundResp.Status),
		)
		// Mark this hub2 payment as caas-funded so duplicate webhooks are skipped.
		if err := q.MarkHub2PaymentCaasFunded(ctx, payment.ID); err != nil {
			s.logger.Error("mark hub2 payment caas-funded",
				zap.String("reference", payload.Reference), zap.Error(err))
		}
	}

	// Record a pending deposit in local history so the customer sees it
	// immediately. Balance in GET /crypto/balances is live from CaaS and will
	// update once the full conversion settles (steps 7-8).
	if user.ID != uuid.Nil {
		ref := fmt.Sprintf("DEP-%s", payload.Reference)
		if _, txErr := q.CreateCryptoTransaction(ctx, db.CreateCryptoTransactionParams{
			Reference:     ref,
			SenderUserID:  user.ID,
			ReceiverPhone: phone,
			Token:         string(caas.TokenUSDC),
			Amount:        quote.ExpectedOut,
			Status:        "pending",
		}); txErr != nil {
			s.logger.Error("record pending deposit transaction",
				zap.String("ref", ref),
				zap.Error(txErr),
			)
		}
	}

	// HUB2's side is done — mark the payment completed.
	if _, err = q.UpdateHub2PaymentStatus(ctx, db.UpdateHub2PaymentStatusParams{
		ID:         payment.ID,
		Status:     "completed",
		Hub2Status: &payload.Status,
	}); err != nil {
		s.logger.Error("update hub2 payment to completed", zap.Error(err))
	}

	s.logger.Info("hub2 collection processed — CaaS notified, awaiting fiat settlement",
		zap.String("reference", payload.Reference),
		zap.String("phone", phone),
		zap.String("usdc_owed", quote.ExpectedOut),
	)

	return nil
}

func mapHub2Status(hub2Status string) string {
	switch hub2Status {
	case "SUCCESSFUL":
		return "completed"
	case "FAILED", "CANCELLED":
		return "failed"
	default:
		return "pending"
	}
}
