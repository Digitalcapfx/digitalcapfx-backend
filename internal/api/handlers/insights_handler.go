package handlers

import (
	"net/http"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type InsightsHandler struct {
	svc *services.Services
}

func NewInsightsHandler(svc *services.Services) *InsightsHandler {
	return &InsightsHandler{svc: svc}
}

// GetInsights godoc
//
//	@Summary      Financial Insights
//	@Description  Full analytics payload for the Financial Insights screen. Returns: summary cards (total balance, income, spending, net flow), balance trend data points (Fiat + Crypto lines) for chart rendering, asset allocation (fiat % vs crypto %), monthly cash flow (last 6 months income vs spending), and spending breakdown by type (Send/Exchange/Withdraw/Deposit split by fiat vs crypto).
//	@Tags         insights
//	@Produce      json
//	@Security     BearerAuth
//	@Param        period  query  string  false  "Time period"  Enums(1w, 1m, 3m, 6m)
//	@Success      200     {object}  map[string]any
//	@Failure      401     {object}  ErrorResponse
//	@Router       /insights [get]
func (h *InsightsHandler) GetInsights(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	period := r.URL.Query().Get("period")
	data, err := h.svc.Insights.GetInsights(r.Context(), userID, period)
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, data)
}
