package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type CardsHandler struct {
	svc *services.CardService
}

func NewCardsHandler(svc *services.CardService) *CardsHandler {
	return &CardsHandler{svc: svc}
}

// CreateCard creates a new virtual card. Max 3 per user.
// @Summary Create virtual card
// @Tags Cards
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param req body CreateCardRequest true "Card creation details"
// @Success 201 {object} VirtualCardResponse
// @Failure 400 {object} ErrorResponse
// @Router /cards [post]
func (h *CardsHandler) CreateCard(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "Unauthorized")
		return
	}

	var req CreateCardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "BAD_REQUEST", "invalid request body")
		return
	}

	if req.Name == "" {
		response.BadRequest(w, "BAD_REQUEST", "name is required")
		return
	}

	var fAccID, fWalID *uuid.UUID
	if req.FundingAccountID != nil {
		id, err := uuid.Parse(*req.FundingAccountID)
		if err == nil {
			fAccID = &id
		}
	}
	if req.FundingWalletID != nil {
		id, err := uuid.Parse(*req.FundingWalletID)
		if err == nil {
			fWalID = &id
		}
	}

	card, err := h.svc.CreateVirtualCard(r.Context(), userID, fAccID, fWalID, req.Name, req.ColorTheme, req.CardArtID)
	if err != nil {
		response.BadRequest(w, "BAD_REQUEST", err.Error())
		return
	}

	response.Created(w, VirtualCardData{
		ID:         card.ID.String(),
		Name:       card.CardName,
		MaskedPan:  derefStr(card.MaskedPan, ""),
		Expiry:     derefStr(card.Expiry, ""),
		ColorTheme: derefStr(card.ColorTheme, ""),
		CardArtID:  derefStr(card.CardArtID, ""),
		Status:     card.Status,
		CreatedAt:  card.CreatedAt,
	})
}

// ListCards lists all virtual cards for the logged in user.
// @Summary List virtual cards
// @Tags Cards
// @Produce json
// @Security BearerAuth
// @Success 200 {object} VirtualCardListResponse
// @Router /cards [get]
func (h *CardsHandler) ListCards(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "Unauthorized")
		return
	}

	cards, err := h.svc.ListVirtualCards(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	data := make([]VirtualCardData, 0, len(cards))
	for _, card := range cards {
		data = append(data, VirtualCardData{
			ID:         card.ID.String(),
			Name:       card.CardName,
			MaskedPan:  derefStr(card.MaskedPan, ""),
			Expiry:     derefStr(card.Expiry, ""),
			ColorTheme: derefStr(card.ColorTheme, ""),
			CardArtID:  derefStr(card.CardArtID, ""),
			Status:     card.Status,
			CreatedAt:  card.CreatedAt,
		})
	}
	response.OK(w, data)
}

// UpdateCard updates card details or freezes it.
// @Summary Update virtual card
// @Tags Cards
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Card ID"
// @Param req body UpdateCardRequest true "Update payload"
// @Success 200 {object} VirtualCardResponse
// @Router /cards/{id} [patch]
func (h *CardsHandler) UpdateCard(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "Unauthorized")
		return
	}

	cardIDStr := chi.URLParam(r, "id")
	cardID, err := uuid.Parse(cardIDStr)
	if err != nil {
		response.BadRequest(w, "BAD_REQUEST", "invalid card ID")
		return
	}

	var req UpdateCardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "BAD_REQUEST", "invalid request body")
		return
	}

	card, err := h.svc.UpdateVirtualCard(r.Context(), userID, cardID, req.Name, req.ColorTheme, req.Status)
	if err != nil {
		response.BadRequest(w, "BAD_REQUEST", err.Error())
		return
	}

	response.OK(w, VirtualCardData{
		ID:         card.ID.String(),
		Name:       card.CardName,
		MaskedPan:  derefStr(card.MaskedPan, ""),
		Expiry:     derefStr(card.Expiry, ""),
		ColorTheme: derefStr(card.ColorTheme, ""),
		CardArtID:  derefStr(card.CardArtID, ""),
		Status:     card.Status,
		CreatedAt:  card.CreatedAt,
	})
}
