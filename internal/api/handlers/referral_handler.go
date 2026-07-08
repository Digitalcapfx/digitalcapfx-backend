package handlers

import (
	"net/http"
	"strconv"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type ReferralHandler struct {
	svc *services.Services
}

func NewReferralHandler(svc *services.Services) *ReferralHandler {
	return &ReferralHandler{svc: svc}
}

// GetReferralData godoc
//
//	@Summary      Get referral details
//	@Description  Returns the authenticated user's referral code, aggregate reward points, total referrals count, and list of referred users.
//	@Tags         referrals
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  services.ReferralData
//	@Failure      401  {object}  ErrorResponse
//	@Failure      500  {object}  ErrorResponse
//	@Router       /referrals [get]
func (h *ReferralHandler) GetReferralData(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	data, err := h.svc.Referral.GetReferralData(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, data)
}

// GetPointsHistory godoc
//
//	@Summary      Get reward points history
//	@Description  Returns a paginated list of point ledger entries (credits and debits) for the authenticated user.
//	@Tags         referrals
//	@Produce      json
//	@Security     BearerAuth
//	@Param        page   query     int  false  "Page number (default 1)"
//	@Param        limit  query     int  false  "Page limit (default 20)"
//	@Success      200    {object}  services.PointsHistoryResponse
//	@Failure      401    {object}  ErrorResponse
//	@Failure      500    {object}  ErrorResponse
//	@Router       /referrals/points/ledger [get]
func (h *ReferralHandler) GetPointsHistory(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	history, err := h.svc.Referral.GetPointsHistory(r.Context(), userID, int32(page), int32(limit))
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, history)
}