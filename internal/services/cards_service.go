package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
	"go.uber.org/zap"
)

type CardService struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

func NewCardService(pool *pgxpool.Pool, logger *zap.Logger) *CardService {
	return &CardService{
		pool:   pool,
		logger: logger,
	}
}

func (s *CardService) CreateVirtualCard(ctx context.Context, userID uuid.UUID, fundingAccountID, fundingWalletID *uuid.UUID, name, colorTheme, cardArtID string) (*db.VirtualCard, error) {
	q := db.New(s.pool)
	// Enforce limit: Max 3 active cards per user
	count, err := q.CountActiveVirtualCards(ctx, userID)
	if err != nil {
		s.logger.Error("failed to count active cards", zap.Error(err), zap.String("user_id", userID.String()))
		return nil, errors.New("failed to verify card limits")
	}

	if count >= 3 {
		return nil, errors.New("maximum number of active virtual cards (3) reached")
	}

	// MOCK: Generate fake PAN details
	// In reality, you call your card provider API here
	maskedPan := fmt.Sprintf("**** **** **** %04d", time.Now().UnixNano()%10000)
	expiry := "12/28"
	cvvEncrypted := "encrypted_123" // Normally encrypt before storing
	providerCardID := fmt.Sprintf("mock_card_%d", time.Now().UnixNano())

	card, err := q.CreateVirtualCard(ctx, db.CreateVirtualCardParams{
		UserID:           userID,
		FundingAccountID: fundingAccountID,
		FundingWalletID:  fundingWalletID,
		CardName:         name,
		ColorTheme:       ptrString(colorTheme),
		CardArtID:        ptrString(cardArtID),
		MaskedPan:        ptrString(maskedPan),
		Expiry:           ptrString(expiry),
		CvvEncrypted:     ptrString(cvvEncrypted),
		Status:           "active",
		ProviderCardID:   ptrString(providerCardID),
	})
	if err != nil {
		s.logger.Error("failed to create virtual card", zap.Error(err))
		return nil, errors.New("failed to create virtual card")
	}

	return &card, nil
}

func (s *CardService) ListVirtualCards(ctx context.Context, userID uuid.UUID) ([]db.VirtualCard, error) {
	q := db.New(s.pool)
	cards, err := q.ListVirtualCardsByUserID(ctx, userID)
	if err != nil {
		s.logger.Error("failed to list virtual cards", zap.Error(err))
		return nil, errors.New("failed to retrieve cards")
	}
	return cards, nil
}

func (s *CardService) GetVirtualCard(ctx context.Context, userID, cardID uuid.UUID) (*db.VirtualCard, error) {
	q := db.New(s.pool)
	card, err := q.GetVirtualCardByID(ctx, db.GetVirtualCardByIDParams{
		ID:     cardID,
		UserID: userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("virtual card not found")
		}
		s.logger.Error("failed to get virtual card", zap.Error(err))
		return nil, errors.New("failed to retrieve card")
	}
	return &card, nil
}

func (s *CardService) UpdateVirtualCard(ctx context.Context, userID, cardID uuid.UUID, name, colorTheme, status *string) (*db.VirtualCard, error) {
	// First ensure the card exists and belongs to the user
	_, err := s.GetVirtualCard(ctx, userID, cardID)
	if err != nil {
		return nil, err
	}

	q := db.New(s.pool)
	card, err := q.UpdateVirtualCard(ctx, db.UpdateVirtualCardParams{
		ID:         cardID,
		UserID:     userID,
		CardName:   ptrStringFromPointer(name),
		ColorTheme: ptrStringFromPointer(colorTheme),
		Status:     ptrStringFromPointer(status),
	})
	if err != nil {
		s.logger.Error("failed to update virtual card", zap.Error(err))
		return nil, errors.New("failed to update card")
	}

	return &card, nil
}

func (s *CardService) ListCardTransactions(ctx context.Context, userID, cardID uuid.UUID) ([]db.CardTransaction, error) {
	// Verify ownership first
	_, err := s.GetVirtualCard(ctx, userID, cardID)
	if err != nil {
		return nil, err
	}

	q := db.New(s.pool)
	txs, err := q.ListCardTransactions(ctx, cardID)
	if err != nil {
		s.logger.Error("failed to list card transactions", zap.Error(err))
		return nil, errors.New("failed to retrieve card transactions")
	}
	return txs, nil
}

func ptrStringFromPointer(v *string) *string {
	if v == nil {
		return nil
	}
	return v
}
