package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type AdminUsersHandler struct {
	svc *services.Services
}

func NewAdminUsersHandler(svc *services.Services) *AdminUsersHandler {
	return &AdminUsersHandler{svc: svc}
}

// AdminDashboard godoc
//
//	@Summary      Admin dashboard statistics
//	@Description  Returns aggregate platform stats: total/active/disabled users, KYC pipeline counts, transaction volume (30d), new user growth (7d / 30d), and active staff count.
//	@Tags         admin-users
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  map[string]any
//	@Failure      403  {object}  ErrorResponse
//	@Router       /admin/dashboard [get]
func (h *AdminUsersHandler) AdminDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := h.svc.UserManagement.GetDashboard(r.Context())
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, data)
}

// ListUsers godoc
//
//	@Summary      List all platform users
//	@Description  Full user list with search, KYC status filter, and active/disabled filter. Returns paginated AdminUserView items. Staff can only see users (not each other's admin accounts — those are in /admin/staff).
//	@Tags         admin-users
//	@Produce      json
//	@Security     BearerAuth
//	@Param        search      query  string  false  "Search by name, email, or phone"
//	@Param        kyc_status  query  string  false  "Filter"  Enums(pending, under_review, approved, rejected)
//	@Param        active      query  string  false  "Filter"  Enums(true, false)
//	@Param        page        query  int     false  "Page"
//	@Param        limit       query  int     false  "Results per page"
//	@Success      200  {object}  map[string]any
//	@Failure      403  {object}  ErrorResponse
//	@Router       /admin/users [get]
func (h *AdminUsersHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	filters := services.AdminUserListFilters{
		Search:    r.URL.Query().Get("search"),
		KycStatus: r.URL.Query().Get("kyc_status"),
	}
	if raw := r.URL.Query().Get("active"); raw == "true" {
		t := true
		filters.IsActive = &t
	} else if raw == "false" {
		f := false
		filters.IsActive = &f
	}
	if p, _ := strconv.Atoi(r.URL.Query().Get("page")); p > 0 {
		filters.Page = int32(p)
	}
	if l, _ := strconv.Atoi(r.URL.Query().Get("limit")); l > 0 {
		filters.Limit = int32(l)
	}

	result, err := h.svc.UserManagement.ListUsers(r.Context(), filters)
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, result)
}

// GetUser godoc
//
//	@Summary      Get full user detail
//	@Description  Returns complete user profile including fiat accounts, WaaS wallets, CaaS wallet status, KYC state, and lifetime transaction stats.
//	@Tags         admin-users
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      string  true  "User ID"
//	@Success      200  {object}  map[string]any
//	@Failure      404  {object}  ErrorResponse
//	@Router       /admin/users/{id} [get]
func (h *AdminUsersHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid user id")
		return
	}
	detail, err := h.svc.UserManagement.GetUserDetail(r.Context(), userID)
	if err != nil {
		response.NotFound(w, "user not found")
		return
	}
	response.OK(w, detail)
}

// DisableUser godoc
//
//	@Summary      Disable a user account
//	@Description  Soft-disables the user (sets is_active = false). All subsequent auth attempts are blocked. Existing sessions are invalidated. A reason is logged in the audit trail. The account can be re-enabled at any time.
//	@Tags         admin-users
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      string              true  "User ID"
//	@Param        body  body      DisableUserRequest  false "Optional reason"
//	@Success      200   {object}  MessageResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      404   {object}  ErrorResponse
//	@Router       /admin/users/{id}/disable [post]
func (h *AdminUsersHandler) DisableUser(w http.ResponseWriter, r *http.Request) {
	set, ok := middleware.StaffPermissionsFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "staff permissions not loaded")
		return
	}

	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid user id")
		return
	}

	var body DisableUserRequest
	_ = decodeOptionalJSON(r, &body) // reason is optional

	switch err := h.svc.UserManagement.DisableUser(r.Context(), userID); err {
	case nil:
	case services.ErrUserNotFound:
		response.NotFound(w, err.Error())
		return
	case services.ErrUserAlreadyDisabled:
		response.BadRequest(w, "ALREADY_DISABLED", err.Error())
		return
	default:
		response.InternalError(w)
		return
	}

	h.svc.Staff.LogAction(r.Context(), set, "user.disable", "user", userID.String(), map[string]any{
		"reason": body.Reason,
	}, r.RemoteAddr)

	response.OKWithMessage(w, "user account has been disabled", nil)
}

// EnableUser godoc
//
//	@Summary      Re-enable a disabled user account
//	@Description  Restores access for a previously disabled user. Does not restore any suspended sessions — the user must log in fresh.
//	@Tags         admin-users
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      string  true  "User ID"
//	@Success      200  {object}  MessageResponse
//	@Failure      404  {object}  ErrorResponse
//	@Router       /admin/users/{id}/enable [post]
func (h *AdminUsersHandler) EnableUser(w http.ResponseWriter, r *http.Request) {
	set, ok := middleware.StaffPermissionsFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "staff permissions not loaded")
		return
	}

	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid user id")
		return
	}

	switch err := h.svc.UserManagement.EnableUser(r.Context(), userID); err {
	case nil:
	case services.ErrUserNotFound:
		response.NotFound(w, err.Error())
		return
	case services.ErrUserAlreadyActive:
		response.BadRequest(w, "ALREADY_ACTIVE", err.Error())
		return
	default:
		response.InternalError(w)
		return
	}

	h.svc.Staff.LogAction(r.Context(), set, "user.enable", "user", userID.String(), nil, r.RemoteAddr)
	response.OKWithMessage(w, "user account has been re-enabled", nil)
}

// ResetUserKYC godoc
//
//	@Summary      Reset a user's KYC status
//	@Description  Sets the user's kyc_status back to "pending" so they can resubmit identity documents. Use when documents are unreadable or the wrong document type was submitted.
//	@Tags         admin-users
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      string  true  "User ID"
//	@Success      200  {object}  MessageResponse
//	@Failure      404  {object}  ErrorResponse
//	@Router       /admin/users/{id}/kyc/reset [post]
func (h *AdminUsersHandler) ResetUserKYC(w http.ResponseWriter, r *http.Request) {
	set, ok := middleware.StaffPermissionsFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "staff permissions not loaded")
		return
	}

	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid user id")
		return
	}

	switch err := h.svc.UserManagement.ResetKYC(r.Context(), userID); err {
	case nil:
	case services.ErrUserNotFound:
		response.NotFound(w, err.Error())
		return
	default:
		response.InternalError(w)
		return
	}

	h.svc.Staff.LogAction(r.Context(), set, "kyc.reset", "kyc", userID.String(), nil, r.RemoteAddr)
	response.OKWithMessage(w, "KYC status reset — user can resubmit documents", nil)
}

// ListUserTransactions godoc
//
//	@Summary      List transactions for a specific user (admin view)
//	@Description  Returns paginated transaction history across all fiat accounts for the user. Used by compliance and support staff for investigation.
//	@Tags         admin-users
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id     path   string  true   "User ID"
//	@Param        page   query  int     false  "Page"
//	@Param        limit  query  int     false  "Results per page"
//	@Success      200    {object}  map[string]any
//	@Failure      404    {object}  ErrorResponse
//	@Router       /admin/users/{id}/transactions [get]
func (h *AdminUsersHandler) ListUserTransactions(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid user id")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	txns, total, err := h.svc.UserManagement.GetUserTransactions(r.Context(), userID, int32(page), int32(limit))
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, map[string]any{
		"transactions": txns,
		"total":        total,
		"page":         page,
		"limit":        limit,
	})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func decodeOptionalJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}
