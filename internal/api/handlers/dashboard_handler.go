package handlers

import (
	"net/http"
	"strconv"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type DashboardHandler struct {
	svc *services.Services
}

func NewDashboardHandler(svc *services.Services) *DashboardHandler {
	return &DashboardHandler{svc: svc}
}

// GetDashboard godoc
//
//	@Summary      Home dashboard
//	@Description  Single aggregate endpoint for the home screen. Returns asset allocation (fiat + crypto), fiat wallet balances (Nilos), CaaS USDC balance (Phone Send), this month's income/spending summary, virtual card details, recent P2P contacts, and a unified activity feed.
//	@Tags         dashboard
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  object
//	@Failure      401  {object}  ErrorResponse
//	@Failure      403  {object}  ErrorResponse  "KYC not approved"
//	@Failure      500  {object}  ErrorResponse
//	@Router       /dashboard [get]
func (h *DashboardHandler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	data, err := h.svc.Dashboard.GetDashboard(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, data)
}

// GetActivityFeed godoc
//
//	@Summary      Unified activity feed
//	@Description  Paginated list of all transactions across fiat (Nilos), crypto (Rach Payments WaaS), and stablecoin (Rach CaaS). Each item carries type (credit/debit/exchange), asset symbol, formatted amount, status, and counterparty name where available.
//	@Tags         dashboard
//	@Produce      json
//	@Security     BearerAuth
//	@Param        page      query  int  false  "Page number (default 1)"
//	@Param        per_page  query  int  false  "Items per page (default 20, max 50)"
//	@Success      200  {object}  object
//	@Failure      401  {object}  ErrorResponse
//	@Router       /activity [get]
func (h *DashboardHandler) GetActivityFeed(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	page := int32(1)
	perPage := int32(20)

	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = int32(n)
		}
	}
	if v := r.URL.Query().Get("per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 50 {
			perPage = int32(n)
		}
	}

	result, err := h.svc.Dashboard.GetActivityFeed(r.Context(), userID, page, perPage)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, result)
}

// GetRecentContacts godoc
//
//	@Summary      Recent Phone Send contacts
//	@Description  Returns the 8 most recent recipients of CaaS phone-to-phone stablecoin transfers. Used to populate the quick-pick contact chips in the Phone Send widget.
//	@Tags         dashboard
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  object
//	@Failure      401  {object}  ErrorResponse
//	@Router       /crypto/contacts [get]
func (h *DashboardHandler) GetRecentContacts(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	contacts, err := h.svc.Dashboard.GetRecentContacts(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, contacts)
}
