package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type SupportHandler struct {
	svc *services.Services
}

func NewSupportHandler(svc *services.Services) *SupportHandler {
	return &SupportHandler{svc: svc}
}

// ─── FAQs ─────────────────────────────────────────────────────────────────────

// ListFAQs godoc
//
//	@Summary      List FAQs
//	@Description  Returns active FAQ entries. Pass ?category= to filter (general | account | payment | kyc | technical | card).
//	@Tags         support
//	@Produce      json
//	@Security     BearerAuth
//	@Param        category  query   string  false  "FAQ category"
//	@Success      200       {array} map[string]any
//	@Failure      401       {object}  ErrorResponse
//	@Router       /support/faqs [get]
func (h *SupportHandler) ListFAQs(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	faqs, err := h.svc.Support.ListFAQs(r.Context(), category)
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, faqs)
}

// ─── App Links ────────────────────────────────────────────────────────────────

// GetAppLinks godoc
//
//	@Summary      Get app links
//	@Description  Returns URLs for Privacy Policy, Help Center, and Terms of Use.
//	@Tags         support
//	@Produce      json
//	@Success      200  {object}  map[string]string
//	@Router       /support/links [get]
func (h *SupportHandler) GetAppLinks(w http.ResponseWriter, r *http.Request) {
	response.OK(w, h.svc.Support.GetAppLinks())
}

// ─── Support Tickets ──────────────────────────────────────────────────────────

// CreateTicket godoc
//
//	@Summary      Open a support ticket
//	@Description  Creates a new support ticket with an opening message. Category: general | account | payment | kyc | technical | card.
//	@Tags         support
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      CreateTicketRequest  true  "Ticket details"
//	@Success      201   {object}  map[string]any
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Router       /support/tickets [post]
func (h *SupportHandler) CreateTicket(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body struct {
		Subject  string `json:"subject"`
		Category string `json:"category"`
		Body     string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.Subject == "" || body.Body == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "subject and body are required")
		return
	}
	if body.Category == "" {
		body.Category = "general"
	}

	ticket, err := h.svc.Support.CreateTicket(r.Context(), services.CreateTicketInput{
		UserID:   userID,
		Subject:  body.Subject,
		Category: body.Category,
		Body:     body.Body,
	})
	if err != nil {
		response.InternalError(w)
		return
	}
	response.Created(w, ticket)
}

// ListTickets godoc
//
//	@Summary      List support tickets
//	@Description  Returns the authenticated user's support tickets, newest first.
//	@Tags         support
//	@Produce      json
//	@Security     BearerAuth
//	@Param        page   query   int  false  "Page number (default 1)"
//	@Param        limit  query   int  false  "Results per page (default 20)"
//	@Success      200    {object}  map[string]any
//	@Failure      401    {object}  ErrorResponse
//	@Router       /support/tickets [get]
func (h *SupportHandler) ListTickets(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	tickets, total, err := h.svc.Support.ListTickets(r.Context(), services.ListTicketsInput{
		UserID: userID,
		Page:   int32(page),
		Limit:  int32(limit),
	})
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, map[string]any{
		"tickets": tickets,
		"total":   total,
	})
}

// GetTicket godoc
//
//	@Summary      Get ticket with messages
//	@Description  Returns a specific support ticket and its full message thread.
//	@Tags         support
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      string  true  "Ticket ID"
//	@Success      200  {object}  map[string]any
//	@Failure      401  {object}  ErrorResponse
//	@Failure      404  {object}  ErrorResponse
//	@Router       /support/tickets/{id} [get]
func (h *SupportHandler) GetTicket(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	ticketID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid ticket id")
		return
	}

	ticket, err := h.svc.Support.GetTicket(r.Context(), userID, ticketID)
	if errors.Is(err, services.ErrTicketNotFound) {
		response.NotFound(w, "ticket not found")
		return
	}
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, ticket)
}

// ReplyToTicket godoc
//
//	@Summary      Reply to a ticket
//	@Description  Adds a message to an open support ticket.
//	@Tags         support
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      string             true  "Ticket ID"
//	@Param        body  body      map[string]string  true  "{ body }"
//	@Success      201   {object}  map[string]any
//	@Failure      400   {object}  ErrorResponse  "Ticket resolved/closed"
//	@Failure      401   {object}  ErrorResponse
//	@Failure      404   {object}  ErrorResponse
//	@Router       /support/tickets/{id}/messages [post]
func (h *SupportHandler) ReplyToTicket(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	ticketID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid ticket id")
		return
	}

	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Body == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "body is required")
		return
	}

	msg, err := h.svc.Support.ReplyToTicket(r.Context(), userID, ticketID, body.Body)
	switch {
	case errors.Is(err, services.ErrTicketNotFound):
		response.NotFound(w, "ticket not found")
	case errors.Is(err, services.ErrTicketClosed):
		response.BadRequest(w, "TICKET_CLOSED", err.Error())
	case err != nil:
		response.InternalError(w)
	default:
		response.Created(w, msg)
	}
}
