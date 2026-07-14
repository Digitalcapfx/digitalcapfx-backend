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
	NotifLoginDetected    = "login_detected"
	NotifWelcome          = "welcome"
	NotifKYCSubmitted     = "kyc_submitted"
	NotifKYCApproved      = "kyc_approved"
	NotifKYCRejected      = "kyc_rejected"
	NotifTransferSent     = "transfer_sent"
	NotifTransferRecvd    = "transfer_received"
	NotifDepositRecvd     = "deposit_received"
	NotifDepositConfirmed = "deposit_confirmed"
	NotifWithdrawal       = "withdrawal_processed"
	NotifCryptoSent       = "crypto_sent"
	NotifCryptoRecvd      = "crypto_received"
	NotifExchange         = "exchange_completed"
)

type NotificationService struct {
	pool   *pgxpool.Pool
	fcm    *FCMService // nil when push is not configured
	logger *zap.Logger
}

func NewNotificationService(pool *pgxpool.Pool, fcm *FCMService, logger *zap.Logger) *NotificationService {
	return &NotificationService{pool: pool, fcm: fcm, logger: logger}
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

	// Mirror the in-app notification to the user's mobile devices via FCM.
	// Best-effort and asynchronous — a push failure must never affect the caller.
	if s.fcm.Enabled() {
		go s.pushToUser(context.Background(), in)
	}
}

// pushToUser delivers a notification to all of a user's registered devices and
// prunes any tokens FCM reports as invalid.
func (s *NotificationService) pushToUser(ctx context.Context, in CreateNotificationInput) {
	q := db.New(s.pool)
	tokens, err := q.ListDeviceTokensByUser(ctx, in.UserID)
	if err != nil || len(tokens) == 0 {
		return
	}

	// Carry the type + metadata as data so the app can deep-link.
	data := map[string]string{"type": in.Type}
	for k, v := range in.Metadata {
		data[k] = v
	}

	invalid, err := s.fcm.SendMulticast(ctx, tokens, in.Title, in.Body, data)
	if err != nil {
		s.logger.Warn("fcm push failed", zap.String("type", in.Type), zap.Error(err))
	}
	if len(invalid) > 0 {
		if delErr := q.DeleteDeviceTokens(ctx, invalid); delErr != nil {
			s.logger.Warn("prune invalid device tokens failed", zap.Error(delErr))
		}
	}
}

// CreateForPhone resolves a phone number to a user (any format) and records a
// notification for them. Used by inbound webhooks that identify the customer by
// phone. No-op if the phone doesn't match a user.
func (s *NotificationService) CreateForPhone(ctx context.Context, phone string, in CreateNotificationInput) {
	user, err := db.New(s.pool).GetUserByPhoneAny(ctx, phoneCandidates(phone))
	if err != nil {
		return
	}
	in.UserID = user.ID
	s.Create(ctx, in)
}

// ─── Device token management (push registration) ───────────────────────────────

// RegisterDevice stores (or re-assigns) a device's FCM registration token for a
// user. Called by the app after obtaining a token (login / app start).
func (s *NotificationService) RegisterDevice(ctx context.Context, userID uuid.UUID, token, platform string) error {
	if token == "" {
		return fmt.Errorf("device token is required")
	}
	if platform == "" {
		platform = "unknown"
	}
	return db.New(s.pool).UpsertDeviceToken(ctx, db.UpsertDeviceTokenParams{
		UserID:   userID,
		Token:    token,
		Platform: platform,
	})
}

// UnregisterDevice removes a device token (called on logout / disable push).
func (s *NotificationService) UnregisterDevice(ctx context.Context, userID uuid.UUID, token string) error {
	return db.New(s.pool).DeleteDeviceToken(ctx, db.DeleteDeviceTokenParams{Token: token, UserID: userID})
}

// SendTestPush pushes a test notification to all of a user's devices so the
// mobile team can verify delivery end-to-end.
func (s *NotificationService) SendTestPush(ctx context.Context, userID uuid.UUID) (int, error) {
	if !s.fcm.Enabled() {
		return 0, fmt.Errorf("push notifications are not configured on this environment")
	}
	tokens, err := db.New(s.pool).ListDeviceTokensByUser(ctx, userID)
	if err != nil {
		return 0, err
	}
	if len(tokens) == 0 {
		return 0, fmt.Errorf("no registered devices for this user")
	}
	invalid, err := s.fcm.SendMulticast(ctx, tokens, "DigitalFX test notification",
		"🎉 Push notifications are working on your device.", map[string]string{"type": "test"})
	if len(invalid) > 0 {
		_ = db.New(s.pool).DeleteDeviceTokens(ctx, invalid)
	}
	return len(tokens) - len(invalid), err
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

	total, _ := q.CountNotifications(ctx, db.CountNotificationsParams{UserID: userID, UnreadOnly: unreadOnly})
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
