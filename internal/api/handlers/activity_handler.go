package handlers

import (
	"net/http"
	"strconv"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type ActivityHandler struct {
	svc *services.Services
}

func NewActivityHandler(svc *services.Services) *ActivityHandler {
	return &ActivityHandler{svc: svc}
}

// GetFeed godoc
//
//	@Summary      Activity feed
//	@Description  Unified, filterable transaction feed across all wallet types (fiat, crypto, stablecoin). Results are grouped by calendar day (Today / Yesterday / Monday / …). Supports a type filter tab and full-text search.
//	@Tags         activity
//	@Produce      json
//	@Security     BearerAuth
//	@Param        type    query  string  false  "Filter"  Enums(sent, received, exchanged, deposited, withdrawn)
//	@Param        search  query  string  false  "Search description, asset, or counterparty"
//	@Param        page    query  int     false  "Page (default 1)"
//	@Param        limit   query  int     false  "Results per page (default 20)"
//	@Success      200     {object}  map[string]any
//	@Failure      401     {object}  ErrorResponse
//	@Router       /activity [get]
func (h *ActivityHandler) GetFeed(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	typeFilter := r.URL.Query().Get("type")
	search := r.URL.Query().Get("search")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	feed, err := h.svc.Activity.GetFeed(r.Context(), userID, typeFilter, search, int32(page), int32(limit))
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, feed)
}
