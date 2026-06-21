package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/hub2"
	"github.com/rachfinance/digitalfx/internal/db/sqlc"
)

type HUB2Service struct {
	pool       *pgxpool.Pool
	hub2Client *hub2.Client
	logger     *zap.Logger
}

func NewHUB2Service(pool *pgxpool.Pool, hub2Client *hub2.Client, logger *zap.Logger) *HUB2Service {
	return &HUB2Service{pool: pool, hub2Client: hub2Client, logger: logger}
}

type Hub2PaymentInput struct {
	UserID        uuid.UUID
	Currency      string   // XAF | XOF
	Amount        float64
	Phone         string
	Operator      string   // Orange | MTN | Wave | Moov | Airtel
	PaymentMethod string   // mobile_money | card
	Direction     string   // collection | disbursement
}

// InitiatePayment starts a HUB2 collection or disbursement request.
func (s *HUB2Service) InitiatePayment(ctx context.Context, in Hub2PaymentInput) (*db.Hub2Payment, error) {
	q := db.New(s.pool)

	var (
		hub2Ref string
		err     error
	)

	switch in.Direction {
	case "collection":
		resp, e := s.hub2Client.Collect(ctx, hub2.CollectRequest{
			Amount:   in.Amount,
			Currency: in.Currency,
			Phone:    in.Phone,
			Operator: in.Operator,
		})
		if e != nil {
			return nil, fmt.Errorf("hub2 collect: %w", e)
		}
		hub2Ref = resp.Reference

	case "disbursement":
		resp, e := s.hub2Client.Disburse(ctx, hub2.DisburseRequest{
			Amount:   in.Amount,
			Currency: in.Currency,
			Phone:    in.Phone,
			Operator: in.Operator,
		})
		if e != nil {
			return nil, fmt.Errorf("hub2 disburse: %w", e)
		}
		hub2Ref = resp.Reference

	default:
		return nil, fmt.Errorf("unknown hub2 direction: %s", in.Direction)
	}

	payment, err := q.CreateHub2Payment(ctx, db.CreateHub2PaymentParams{
		Hub2Reference: hub2Ref,
		PaymentMethod: in.PaymentMethod,
		Operator:      &in.Operator,
		PhoneNumber:   &in.Phone,
	})
	if err != nil {
		return nil, fmt.Errorf("store hub2 payment: %w", err)
	}

	return &payment, nil
}

// HandleWebhook processes status updates pushed by HUB2.
func (s *HUB2Service) HandleWebhook(ctx context.Context, payload hub2.WebhookPayload) error {
	q := db.New(s.pool)

	payment, err := q.GetHub2PaymentByReference(ctx, payload.Reference)
	if err != nil {
		return fmt.Errorf("hub2 payment not found: %s", payload.Reference)
	}

	_, err = q.UpdateHub2PaymentStatus(ctx, db.UpdateHub2PaymentStatusParams{
		ID:         payment.ID,
		Status:     mapHub2Status(payload.Status),
		Hub2Status: &payload.Status,
	})
	return err
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
