package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/config"
	"github.com/rachfinance/digitalfx/internal/db/sqlc"
)

type KYCService struct {
	pool   *pgxpool.Pool
	cfg    *config.Config
	logger *zap.Logger
}

func NewKYCService(pool *pgxpool.Pool, cfg *config.Config, logger *zap.Logger) *KYCService {
	return &KYCService{pool: pool, cfg: cfg, logger: logger}
}

func (s *KYCService) GetStatus(ctx context.Context, userID uuid.UUID) (string, error) {
	q := db.New(s.pool)
	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return "", ErrUserNotFound
	}
	return user.KycStatus, nil
}

func (s *KYCService) ListDocuments(ctx context.Context, userID uuid.UUID) ([]db.KycDocument, error) {
	q := db.New(s.pool)
	return q.GetKYCDocumentsByUserID(ctx, userID)
}

type UploadDocumentInput struct {
	UserID  uuid.UUID
	DocType string
	DocURL  string
}

func (s *KYCService) UploadDocument(ctx context.Context, in UploadDocumentInput) (*db.KycDocument, error) {
	q := db.New(s.pool)

	doc, err := q.CreateKYCDocument(ctx, db.CreateKYCDocumentParams{
		UserID:  in.UserID,
		DocType: in.DocType,
		DocURL:  in.DocURL,
	})
	if err != nil {
		return nil, fmt.Errorf("create kyc document: %w", err)
	}

	// Mark user KYC as submitted once at least one document is uploaded
	if _, err := q.UpdateUserKYCStatus(ctx, db.UpdateUserKYCStatusParams{
		ID:        in.UserID,
		KycStatus: "submitted",
	}); err != nil {
		s.logger.Error("update kyc status", zap.Error(err))
	}

	return &doc, nil
}
