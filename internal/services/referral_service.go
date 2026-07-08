package services

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

type ReferralService struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

func NewReferralService(pool *pgxpool.Pool, logger *zap.Logger) *ReferralService {
	return &ReferralService{pool: pool, logger: logger}
}

type ReferralItem struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	CreatedAt time.Time `json:"created_at"`
}

type ReferralData struct {
	ReferralCode string         `json:"referral_code"`
	Points       int64          `json:"points"`
	Count        int64          `json:"count"`
	Referrals    []ReferralItem `json:"referrals"`
}

type PointsHistoryResponse struct {
	History []db.PointsLedger `json:"history"`
	Page    int32             `json:"page"`
	Limit   int32             `json:"limit"`
}

func (s *ReferralService) GetReferralData(ctx context.Context, userID uuid.UUID) (*ReferralData, error) {
	q := db.New(s.pool)

	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	var code string
	if user.ReferralCode != nil {
		code = *user.ReferralCode
	}

	points, err := q.GetPointsBalance(ctx, userID)
	if err != nil {
		s.logger.Error("failed to get points balance", zap.Error(err))
	}

	count, err := q.GetReferralsCount(ctx, &userID)
	if err != nil {
		s.logger.Error("failed to get referrals count", zap.Error(err))
	}

	list, err := q.GetReferralsList(ctx, &userID)
	if err != nil {
		s.logger.Error("failed to get referrals list", zap.Error(err))
	}

	items := make([]ReferralItem, 0, len(list))
	for _, u := range list {
		var emailStr string
		if u.Email != nil {
			emailStr = *u.Email
		}
		items = append(items, ReferralItem{
			ID:        u.ID,
			Email:     emailStr,
			FirstName: u.FirstName,
			LastName:  u.LastName,
			CreatedAt: u.CreatedAt,
		})
	}

	return &ReferralData{
		ReferralCode: code,
		Points:       points,
		Count:        count,
		Referrals:    items,
	}, nil
}

func (s *ReferralService) GetPointsHistory(ctx context.Context, userID uuid.UUID, page, limit int32) (*PointsHistoryResponse, error) {
	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	q := db.New(s.pool)
	history, err := q.GetPointsHistory(ctx, db.GetPointsHistoryParams{
		UserID: userID,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, err
	}

	return &PointsHistoryResponse{
		History: history,
		Page:    page,
		Limit:   limit,
	}, nil
}