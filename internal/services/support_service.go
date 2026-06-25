package services

import (
	"context"
	"errors"
	"fmt"
	mrand "math/rand"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

var (
	ErrTicketNotFound = errors.New("support ticket not found")
	ErrTicketClosed   = errors.New("this ticket is resolved or closed — open a new one to continue")
)

const (
	PrivacyPolicyURL = "https://digitalfx.rach.finance/legal/privacy"
	HelpCenterURL    = "https://help.digitalfx.rach.finance"
)

type SupportService struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

func NewSupportService(pool *pgxpool.Pool, logger *zap.Logger) *SupportService {
	return &SupportService{pool: pool, logger: logger}
}

// ─── FAQs ─────────────────────────────────────────────────────────────────────

// ListFAQs returns active FAQs, optionally filtered by category.
// Pass an empty string to return all categories.
func (s *SupportService) ListFAQs(ctx context.Context, category string) ([]db.FAQ, error) {
	q := db.New(s.pool)
	return q.ListFAQs(ctx, category)
}

// ─── Tickets ──────────────────────────────────────────────────────────────────

type CreateTicketInput struct {
	UserID   uuid.UUID
	Subject  string
	Category string
	Body     string // opening message from the user
}

func (s *SupportService) CreateTicket(ctx context.Context, in CreateTicketInput) (*db.SupportTicketWithMessages, error) {
	q := db.New(s.pool)

	ref := fmt.Sprintf("TKT-%06d", mrand.Int63n(1000000))

	ticket, err := q.CreateSupportTicket(ctx, db.CreateSupportTicketParams{
		UserID:    in.UserID,
		Reference: ref,
		Subject:   in.Subject,
		Category:  in.Category,
	})
	if err != nil {
		return nil, fmt.Errorf("create ticket: %w", err)
	}

	msg, err := q.CreateSupportMessage(ctx, db.CreateSupportMessageParams{
		TicketID:   ticket.ID,
		SenderType: "user",
		SenderID:   &in.UserID,
		Body:       in.Body,
	})
	if err != nil {
		s.logger.Error("create opening message", zap.String("ticket", ref), zap.Error(err))
	}

	result := &db.SupportTicketWithMessages{SupportTicket: ticket}
	if err == nil {
		result.Messages = []db.SupportMessage{msg}
	}
	return result, nil
}

type ListTicketsInput struct {
	UserID uuid.UUID
	Page   int32
	Limit  int32
}

func (s *SupportService) ListTickets(ctx context.Context, in ListTicketsInput) ([]db.SupportTicket, int64, error) {
	q := db.New(s.pool)

	if in.Limit <= 0 {
		in.Limit = 20
	}
	if in.Page <= 0 {
		in.Page = 1
	}
	offset := (in.Page - 1) * in.Limit

	tickets, err := q.ListSupportTickets(ctx, db.ListSupportTicketsParams{
		UserID: in.UserID,
		Limit:  in.Limit,
		Offset: offset,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list tickets: %w", err)
	}

	total, _ := q.CountSupportTickets(ctx, in.UserID)
	return tickets, total, nil
}

func (s *SupportService) GetTicket(ctx context.Context, userID, ticketID uuid.UUID) (*db.SupportTicketWithMessages, error) {
	q := db.New(s.pool)

	ticket, err := q.GetSupportTicket(ctx, db.GetSupportTicketParams{ID: ticketID, UserID: userID})
	if err != nil {
		return nil, ErrTicketNotFound
	}

	messages, _ := q.ListSupportMessages(ctx, ticketID)
	return &db.SupportTicketWithMessages{
		SupportTicket: ticket,
		Messages:      messages,
	}, nil
}

func (s *SupportService) ReplyToTicket(ctx context.Context, userID, ticketID uuid.UUID, body string) (*db.SupportMessage, error) {
	q := db.New(s.pool)

	ticket, err := q.GetSupportTicket(ctx, db.GetSupportTicketParams{ID: ticketID, UserID: userID})
	if err != nil {
		return nil, ErrTicketNotFound
	}
	if ticket.Status == "closed" || ticket.Status == "resolved" {
		return nil, ErrTicketClosed
	}

	msg, err := q.CreateSupportMessage(ctx, db.CreateSupportMessageParams{
		TicketID:   ticketID,
		SenderType: "user",
		SenderID:   &userID,
		Body:       body,
	})
	if err != nil {
		return nil, fmt.Errorf("reply to ticket: %w", err)
	}
	return &msg, nil
}

// ─── Static Links ─────────────────────────────────────────────────────────────

type AppLinks struct {
	PrivacyPolicy string `json:"privacy_policy_url"`
	HelpCenter    string `json:"help_center_url"`
	TermsOfUse    string `json:"terms_of_use_url"`
}

func (s *SupportService) GetAppLinks() AppLinks {
	return AppLinks{
		PrivacyPolicy: PrivacyPolicyURL,
		HelpCenter:    HelpCenterURL,
		TermsOfUse:    "https://digitalfx.rach.finance/legal/terms",
	}
}
