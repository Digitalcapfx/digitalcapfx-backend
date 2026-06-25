package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

// Notification type constants — used by all callers to fire events.
const (
	NotifLoginDetected   = "login_detected"
	NotifWelcome         = "welcome"
	NotifKYCSubmitted    = "kyc_submitted"
	NotifKYCApproved     = "kyc_approved"
	NotifKYCRejected     = "kyc_rejected"
	NotifTransferSent    = "transfer_sent"
	NotifTransferRecvd   = "transfer_received"
	NotifDepositRecvd    = "deposit_received"
	NotifWithdrawal      = "withdrawal_processed"
	NotifCryptoSent      = "crypto_sent"
	NotifCryptoRecvd     = "crypto_received"
	NotifExchange        = "exchange_completed"
)

type NotificationService struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

func NewNotificationService(pool *pgxpool.Pool, logger *zap.Logger) *NotificationService {
	return &NotificationService{pool: pool, logger: logger}
}

type CreateNotificationInput struct {
	UserID   uuid.UUID
	Type     string
	Title    string
	Body     string
	Metadata map[string]string // serialised to JSONB
}

// Create records a notification. Errors are logged but never propagate to the
// caller — a notification failure must never break a primary operation.
func (s *NotificationService) Create(ctx context.Context, in CreateNotificationInput) {
	var meta json.RawMessage
	if len(in.Metadata) > 0 {
		b, _ := json.Marshal(in.Metadata)
		meta = b
	}
	_, err := db.New(s.pool).CreateNotification(ctx, db.CreateNotificationParams{
		UserID:   in.UserID,
		Type:     in.Type,
		Title:    in.Title,
		Body:     in.Body,
		Metadata: meta,
	})
	if err != nil {
		s.logger.Warn("notification create failed",
			zap.String("type", in.Type),
			zap.String("user_id", in.UserID.String()),
			zap.Error(err),
		)
	}
}

// ─── Read / management ────────────────────────────────────────────────────────

type NotificationListResult struct {
	Items  []db.Notification `json:"items"`
	Unread int64             `json:"unread"`
	Total  int64             `json:"total"`
	Page   int32             `json:"page"`
	Limit  int32             `json:"limit"`
}

func (s *NotificationService) List(ctx context.Context, userID uuid.UUID, page, limit int32, unreadOnly bool) (*NotificationListResult, error) {
	if limit == 0 {
		limit = 20
	}
	if page == 0 {
		page = 1
	}

	q := db.New(s.pool)

	items, err := q.ListNotifications(ctx, db.ListNotificationsParams{
		UserID:     userID,
		UnreadOnly: unreadOnly,
		Limit:      limit,
		Offset:     (page - 1) * limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}

	total, _ := q.CountNotifications(ctx, userID, unreadOnly)
	unread, _ := q.GetUnreadNotificationCount(ctx, userID)

	return &NotificationListResult{
		Items:  items,
		Unread: unread,
		Total:  total,
		Page:   page,
		Limit:  limit,
	}, nil
}

func (s *NotificationService) MarkRead(ctx context.Context, id, userID uuid.UUID) (*db.Notification, error) {
	n, err := db.New(s.pool).MarkNotificationRead(ctx, db.MarkNotificationReadParams{
		ID:     id,
		UserID: userID,
	})
	if err != nil {
		return nil, fmt.Errorf("mark read: %w", err)
	}
	return &n, nil
}

func (s *NotificationService) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	return db.New(s.pool).MarkAllNotificationsRead(ctx, userID)
}

func (s *NotificationService) UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	return db.New(s.pool).GetUnreadNotificationCount(ctx, userID)
}
