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

// AdminLimitsHandler exposes owner/admin control over account-tier limits,
// per-user limit overrides, and a user's account type.
type AdminLimitsHandler struct {
	svc *services.Services
}

func NewAdminLimitsHandler(svc *services.Services) *AdminLimitsHandler {
	return &AdminLimitsHandler{svc: svc}
}

// ListTierLimits godoc
//
//	@Summary      List account-tier limits
//	@Description  Returns the current (admin-editable) limits for every account tier.
//	@Tags         admin-limits
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  map[string]any
//	@Router       /admin/limits [get]
func (h *AdminLimitsHandler) ListTierLimits(w http.ResponseWriter, r *http.Request) {
	tiers, err := h.svc.Limits.TierLimits(r.Context())
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, map[string]any{"tiers": tiers})
}

// UpdateTierLimitsInput is the body for PATCH /admin/limits/{tier}.
type UpdateTierLimitsInput struct {
	DailyWithdrawalUSD    float64 `json:"daily_withdrawal_usd"`
	PerTransactionUSD     float64 `json:"per_transaction_usd"`
	MonthlyVolumeUSD      float64 `json:"monthly_volume_usd"`
	MaxHoldingBalanceUSD  float64 `json:"max_holding_balance_usd"`
	DailyTransactionCount int     `json:"daily_transaction_count"`
}

// UpdateTierLimits godoc
//
//	@Summary      Update an account-tier's limits
//	@Description  Persists new limits for a tier (individual|business). Takes effect within seconds across the platform.
//	@Tags         admin-limits
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        tier  path      string                 true  "Tier"  Enums(individual,business)
//	@Param        body  body      UpdateTierLimitsInput  true  "New limits"
//	@Success      200   {object}  map[string]any
//	@Failure      400   {object}  ErrorResponse
//	@Router       /admin/limits/{tier} [patch]
func (h *AdminLimitsHandler) UpdateTierLimits(w http.ResponseWriter, r *http.Request) {
	set, _ := middleware.StaffPermissionsFromContext(r.Context())
	tier := chi.URLParam(r, "tier")

	var in UpdateTierLimitsInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON payload")
		return
	}

	updatedBy := staffIDPtr(r)
	updated, err := h.svc.Limits.UpdateTier(r.Context(), tier, services.AccountLimits{
		DailyWithdrawalUSD:    in.DailyWithdrawalUSD,
		PerTransactionUSD:     in.PerTransactionUSD,
		MonthlyVolumeUSD:      in.MonthlyVolumeUSD,
		MaxHoldingBalanceUSD:  in.MaxHoldingBalanceUSD,
		DailyTransactionCount: in.DailyTransactionCount,
	}, updatedBy)
	if err != nil {
		response.BadRequest(w, "INVALID_TIER", err.Error())
		return
	}

	h.svc.Staff.LogAction(r.Context(), set, "limits.tier_update", "limits", tier, map[string]any{
		"limits": updated,
	}, r.RemoteAddr)

	response.OK(w, updated)
}

// GetUserLimits godoc
//
//	@Summary      Get a user's limits
//	@Description  Returns a user's effective limits, the tier default they derive from, and any per-user override.
//	@Tags         admin-limits
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id  path      string  true  "User ID"
//	@Success      200  {object}  services.UserLimitsView
//	@Failure      404  {object}  ErrorResponse
//	@Router       /admin/users/{id}/limits [get]
func (h *AdminLimitsHandler) GetUserLimits(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid user id")
		return
	}
	view, err := h.svc.Limits.UserLimits(r.Context(), userID)
	if err != nil {
		response.NotFound(w, "user not found")
		return
	}
	response.OK(w, view)
}

// SetUserLimitsInput is the body for PUT /admin/users/{id}/limits. A null field
// means "inherit the tier limit"; a set field overrides it.
type SetUserLimitsInput struct {
	DailyWithdrawalUSD    *float64 `json:"daily_withdrawal_usd"`
	PerTransactionUSD     *float64 `json:"per_transaction_usd"`
	MonthlyVolumeUSD      *float64 `json:"monthly_volume_usd"`
	MaxHoldingBalanceUSD  *float64 `json:"max_holding_balance_usd"`
	DailyTransactionCount *int     `json:"daily_transaction_count"`
	Note                  string   `json:"note"`
}

// SetUserLimits godoc
//
//	@Summary      Set a user's limit override
//	@Description  Creates or replaces a per-user limit override. Null fields inherit the tier limit.
//	@Tags         admin-limits
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      string              true  "User ID"
//	@Param        body  body      SetUserLimitsInput  true  "Override"
//	@Success      200   {object}  services.UserLimitsView
//	@Failure      400   {object}  ErrorResponse
//	@Router       /admin/users/{id}/limits [put]
func (h *AdminLimitsHandler) SetUserLimits(w http.ResponseWriter, r *http.Request) {
	set, _ := middleware.StaffPermissionsFromContext(r.Context())
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid user id")
		return
	}

	var in SetUserLimitsInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON payload")
		return
	}

	if err := h.svc.Limits.SetUserOverride(r.Context(), userID, services.AccountLimitsOverride{
		DailyWithdrawalUSD:    in.DailyWithdrawalUSD,
		PerTransactionUSD:     in.PerTransactionUSD,
		MonthlyVolumeUSD:      in.MonthlyVolumeUSD,
		MaxHoldingBalanceUSD:  in.MaxHoldingBalanceUSD,
		DailyTransactionCount: in.DailyTransactionCount,
		Note:                  in.Note,
	}, staffIDPtr(r)); err != nil {
		response.InternalError(w)
		return
	}

	view, err := h.svc.Limits.UserLimits(r.Context(), userID)
	if err != nil {
		response.NotFound(w, "user not found")
		return
	}

	h.svc.Staff.LogAction(r.Context(), set, "limits.user_override_set", "user", userID.String(), map[string]any{
		"override": view.Override,
	}, r.RemoteAddr)

	response.OK(w, view)
}

// ClearUserLimits godoc
//
//	@Summary      Clear a user's limit override
//	@Description  Removes a per-user override so the user falls back to their tier limits.
//	@Tags         admin-limits
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id  path  string  true  "User ID"
//	@Success      200  {object}  services.UserLimitsView
//	@Router       /admin/users/{id}/limits [delete]
func (h *AdminLimitsHandler) ClearUserLimits(w http.ResponseWriter, r *http.Request) {
	set, _ := middleware.StaffPermissionsFromContext(r.Context())
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid user id")
		return
	}
	if err := h.svc.Limits.ClearUserOverride(r.Context(), userID); err != nil {
		response.InternalError(w)
		return
	}

	h.svc.Staff.LogAction(r.Context(), set, "limits.user_override_clear", "user", userID.String(), nil, r.RemoteAddr)

	view, err := h.svc.Limits.UserLimits(r.Context(), userID)
	if err != nil {
		response.NotFound(w, "user not found")
		return
	}
	response.OK(w, view)
}

// SetAccountTypeInput is the body for POST /admin/users/{id}/account-type.
type SetAccountTypeInput struct {
	AccountType string `json:"account_type"` // individual | business
}

// SetUserAccountType godoc
//
//	@Summary      Change a user's account type
//	@Description  Switches a user between individual and business tiers (changes their default limits and unlocks/locks business features).
//	@Tags         admin-limits
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      string               true  "User ID"
//	@Param        body  body      SetAccountTypeInput  true  "Account type"
//	@Success      200   {object}  map[string]any
//	@Failure      400   {object}  ErrorResponse
//	@Router       /admin/users/{id}/account-type [post]
func (h *AdminLimitsHandler) SetUserAccountType(w http.ResponseWriter, r *http.Request) {
	set, _ := middleware.StaffPermissionsFromContext(r.Context())
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid user id")
		return
	}

	var in SetAccountTypeInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON payload")
		return
	}

	newType, err := h.svc.Limits.SetAccountType(r.Context(), userID, in.AccountType)
	if err != nil {
		response.BadRequest(w, "INVALID_ACCOUNT_TYPE", err.Error())
		return
	}

	h.svc.Staff.LogAction(r.Context(), set, "user.account_type_change", "user", userID.String(), map[string]any{
		"account_type": newType,
	}, r.RemoteAddr)

	response.OK(w, map[string]any{"user_id": userID.String(), "account_type": newType})
}

// staffIDPtr returns the acting staff member's id (for updated_by audit
// columns), or nil if unavailable (e.g. owner acting via JWT only).
func staffIDPtr(r *http.Request) *uuid.UUID {
	if id, ok := middleware.StaffIDFromContext(r.Context()); ok {
		return &id
	}
	return nil
}
