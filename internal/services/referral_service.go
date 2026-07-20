package services

import (
	"context"
	mrand "math/rand"
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

	// Existing users created before referral codes existed have none — generate
	// and persist one on first access so every user always has a code.
	var code string
	if user.ReferralCode != nil && *user.ReferralCode != "" {
		code = *user.ReferralCode
	} else {
		code = s.ensureReferralCode(ctx, q, user)
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

// ensureReferralCode assigns and persists a unique referral code for a user who
// doesn't have one yet. It retries on the (rare) unique-constraint collision,
// falling back to a fully-random code after a few name-based attempts.
func (s *ReferralService) ensureReferralCode(ctx context.Context, q *db.Queries, user db.User) string {
	for attempt := 0; attempt < 6; attempt++ {
		code := generateReferralCode(user.FirstName, user.LastName)
		if attempt >= 3 {
			code = randomReferralCode() // name-based keeps colliding — go fully random
		}
		if err := q.SetReferralCode(ctx, db.SetReferralCodeParams{ID: user.ID, ReferralCode: &code}); err != nil {
			s.logger.Warn("referral code collision, retrying", zap.Int("attempt", attempt), zap.Error(err))
			continue
		}
		s.logger.Info("assigned referral code", zap.String("user_id", user.ID.String()), zap.String("code", code))
		return code
	}
	s.logger.Error("could not assign referral code", zap.String("user_id", user.ID.String()))
	return ""
}

// randomReferralCode returns a collision-resistant code (no ambiguous chars).
func randomReferralCode() string {
	const cs = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = cs[mrand.Intn(len(cs))]
	}
	return "RF" + string(b)
}
