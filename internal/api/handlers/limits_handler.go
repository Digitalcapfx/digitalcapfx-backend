package handlers

import (
	"net/http"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

// LimitsHandler exposes the caller's account-tier limits and current usage.
type LimitsHandler struct {
	svc *services.Services
}

func NewLimitsHandler(svc *services.Services) *LimitsHandler {
	return &LimitsHandler{svc: svc}
}

// AccountLimitsResponse is the payload for GET /account/limits.
type AccountLimitsResponse struct {
	Limits services.AccountLimits `json:"limits"`
	Usage  LimitsUsage            `json:"usage"`
}

// LimitsUsage reports how much of the caller's limits has been consumed and
// what remains, so a frontend can render progress bars without re-computing.
type LimitsUsage struct {
	DailyWithdrawalUsedUSD      float64 `json:"daily_withdrawal_used_usd"`
	DailyWithdrawalRemainingUSD float64 `json:"daily_withdrawal_remaining_usd"`
}

// GetLimits godoc
//
//	@Summary      Get account limits and usage
//	@Description  Returns the caller's tier limits (individual vs business) and current 24h withdrawal usage.
//	@Tags         account
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  AccountLimitsResponse
//	@Failure      401  {object}  ErrorResponse
//	@Router       /account/limits [get]
func (h *LimitsHandler) GetLimits(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	limits := h.svc.Withdrawal.Limits(r.Context(), userID)
	used := h.svc.Withdrawal.DailyWithdrawalUsedUSD(r.Context(), userID)

	remaining := limits.DailyWithdrawalUSD - used
	if remaining < 0 {
		remaining = 0
	}

	response.OK(w, AccountLimitsResponse{
		Limits: limits,
		Usage: LimitsUsage{
			DailyWithdrawalUsedUSD:      used,
			DailyWithdrawalRemainingUSD: remaining,
		},
	})
}
