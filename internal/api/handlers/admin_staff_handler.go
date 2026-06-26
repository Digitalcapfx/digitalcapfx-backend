package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type AdminStaffHandler struct {
	svc *services.Services
}

func NewAdminStaffHandler(svc *services.Services) *AdminStaffHandler {
	return &AdminStaffHandler{svc: svc}
}

// InviteStaff godoc
//
//	@Summary      Invite a new staff member
//	@Description  Creates a pending staff member record and sends an invitation email. The invitee must click the link in the email to accept and link their account. Only owner and admin roles can invite staff. You cannot invite someone as "owner".
//	@Tags         admin-staff
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      InviteStaffRequest  true  "Staff invitation details"
//	@Success      201   {object}  map[string]any
//	@Failure      400   {object}  ErrorResponse
//	@Failure      403   {object}  ErrorResponse
//	@Router       /admin/staff/invite [post]
func (h *AdminStaffHandler) InviteStaff(w http.ResponseWriter, r *http.Request) {
	set, ok := middleware.StaffPermissionsFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "staff permissions not loaded")
		return
	}

	var body InviteStaffRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.Email == "" || body.Name == "" || body.Role == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "email, name, and role are required")
		return
	}

	inviterID, _ := middleware.StaffIDFromContext(r.Context())

	member, err := h.svc.Staff.InviteStaff(r.Context(), services.InviteStaffInput{
		InviterStaffID: inviterID,
		Email:          body.Email,
		Name:           body.Name,
		Role:           body.Role,
		CustomPerms:    body.CustomPermissions,
		RevokedPerms:   body.RevokedPermissions,
	})
	switch err {
	case nil:
	case services.ErrStaffAlreadyExists:
		response.Conflict(w, "STAFF_EXISTS", err.Error())
		return
	case services.ErrInvalidRole:
		response.BadRequest(w, "INVALID_ROLE", err.Error())
		return
	default:
		response.BadRequest(w, "VALIDATION_ERROR", err.Error())
		return
	}

	h.svc.Staff.LogAction(r.Context(), set, "staff.invite", "staff", member.ID, map[string]any{
		"email": body.Email, "role": body.Role,
	}, r.RemoteAddr)

	response.Created(w, member)
}

// ListStaff godoc
//
//	@Summary      List all staff members
//	@Description  Returns all staff members with their roles, effective permissions, and activity status. Supports optional filtering to include disabled accounts.
//	@Tags         admin-staff
//	@Produce      json
//	@Security     BearerAuth
//	@Param        include_inactive  query  bool   false  "Include disabled staff (default false)"
//	@Param        page              query  int    false  "Page"
//	@Param        limit             query  int    false  "Results per page"
//	@Success      200  {object}  map[string]any
//	@Failure      403  {object}  ErrorResponse
//	@Router       /admin/staff [get]
func (h *AdminStaffHandler) ListStaff(w http.ResponseWriter, r *http.Request) {
	includeInactive := r.URL.Query().Get("include_inactive") == "true"
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	result, err := h.svc.Staff.List(r.Context(), includeInactive, int32(page), int32(limit))
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, result)
}

// GetStaff godoc
//
//	@Summary      Get a staff member
//	@Description  Returns the full profile for a single staff member including their effective permissions (role defaults minus revocations plus custom grants).
//	@Tags         admin-staff
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      string  true  "Staff member ID"
//	@Success      200  {object}  map[string]any
//	@Failure      404  {object}  ErrorResponse
//	@Router       /admin/staff/{id} [get]
func (h *AdminStaffHandler) GetStaff(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid staff id")
		return
	}
	member, err := h.svc.Staff.GetByID(r.Context(), id)
	if err != nil {
		response.NotFound(w, "staff member not found")
		return
	}
	response.OK(w, member)
}

// UpdateStaff godoc
//
//	@Summary      Update a staff member's role or permissions
//	@Description  Changes the role or fine-tunes the permission set (custom grants and revocations) for a staff member. Cannot be used to modify the owner account. Omit a field to leave it unchanged.
//	@Tags         admin-staff
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      string              true  "Staff member ID"
//	@Param        body  body      UpdateStaffRequest  true  "Updated role/permissions"
//	@Success      200   {object}  map[string]any
//	@Failure      400   {object}  ErrorResponse
//	@Failure      403   {object}  ErrorResponse
//	@Failure      404   {object}  ErrorResponse
//	@Router       /admin/staff/{id} [patch]
func (h *AdminStaffHandler) UpdateStaff(w http.ResponseWriter, r *http.Request) {
	set, ok := middleware.StaffPermissionsFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "staff permissions not loaded")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid staff id")
		return
	}

	var body UpdateStaffRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}

	updated, err := h.svc.Staff.Update(r.Context(), id, services.UpdateStaffInput{
		Role:         body.Role,
		CustomPerms:  body.CustomPermissions,
		RevokedPerms: body.RevokedPermissions,
	})
	switch err {
	case nil:
	case services.ErrStaffNotFound:
		response.NotFound(w, err.Error())
		return
	case services.ErrCannotModifyOwner:
		response.Forbidden(w, err.Error())
		return
	case services.ErrInvalidRole:
		response.BadRequest(w, "INVALID_ROLE", err.Error())
		return
	default:
		response.BadRequest(w, "VALIDATION_ERROR", err.Error())
		return
	}

	h.svc.Staff.LogAction(r.Context(), set, "staff.update", "staff", id.String(), map[string]any{
		"role": body.Role,
	}, r.RemoteAddr)

	response.OK(w, updated)
}

// DisableStaff godoc
//
//	@Summary      Disable a staff member
//	@Description  Deactivates a staff member's access. Subsequent API requests from that account will be rejected with 403. The account can be re-enabled at any time. The owner cannot be disabled.
//	@Tags         admin-staff
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      string  true  "Staff member ID"
//	@Success      200  {object}  MessageResponse
//	@Failure      403  {object}  ErrorResponse
//	@Failure      404  {object}  ErrorResponse
//	@Router       /admin/staff/{id}/disable [post]
func (h *AdminStaffHandler) DisableStaff(w http.ResponseWriter, r *http.Request) {
	set, ok := middleware.StaffPermissionsFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "staff permissions not loaded")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid staff id")
		return
	}

	switch err := h.svc.Staff.Disable(r.Context(), id); err {
	case nil:
	case services.ErrStaffNotFound:
		response.NotFound(w, err.Error())
		return
	case services.ErrCannotModifyOwner:
		response.Forbidden(w, err.Error())
		return
	default:
		response.InternalError(w)
		return
	}

	h.svc.Staff.LogAction(r.Context(), set, "staff.disable", "staff", id.String(), nil, r.RemoteAddr)
	response.OKWithMessage(w, "staff member disabled", nil)
}

// EnableStaff godoc
//
//	@Summary      Re-enable a disabled staff member
//	@Description  Restores access for a previously disabled staff member.
//	@Tags         admin-staff
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      string  true  "Staff member ID"
//	@Success      200  {object}  MessageResponse
//	@Failure      404  {object}  ErrorResponse
//	@Router       /admin/staff/{id}/enable [post]
func (h *AdminStaffHandler) EnableStaff(w http.ResponseWriter, r *http.Request) {
	set, ok := middleware.StaffPermissionsFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "staff permissions not loaded")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid staff id")
		return
	}

	switch err := h.svc.Staff.Enable(r.Context(), id); err {
	case nil:
	case services.ErrStaffNotFound:
		response.NotFound(w, err.Error())
		return
	default:
		response.InternalError(w)
		return
	}

	h.svc.Staff.LogAction(r.Context(), set, "staff.enable", "staff", id.String(), nil, r.RemoteAddr)
	response.OKWithMessage(w, "staff member re-enabled", nil)
}

// AcceptInvite godoc
//
//	@Summary      Accept a staff invitation
//	@Description  Links the authenticated user's account to the pending staff member record identified by the invite token. Must be called after login/registration.
//	@Tags         admin-staff
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      AcceptInviteRequest  true  "Invite token"
//	@Success      200   {object}  MessageResponse
//	@Failure      400   {object}  ErrorResponse
//	@Router       /admin/staff/invite/accept [post]
func (h *AdminStaffHandler) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body AcceptInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Token == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "token is required")
		return
	}

	if err := h.svc.Staff.AcceptInvite(r.Context(), body.Token, userID); err != nil {
		response.BadRequest(w, "INVALID_TOKEN", err.Error())
		return
	}
	response.OKWithMessage(w, "invite accepted — staff access is now active", nil)
}

// ListRoles godoc
//
//	@Summary      List available staff roles
//	@Description  Returns all role definitions with their default permissions, labels, and descriptions. Use this to populate the role selector in the invite/edit UI.
//	@Tags         admin-staff
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {array}  map[string]any
//	@Router       /admin/roles [get]
func (h *AdminStaffHandler) ListRoles(w http.ResponseWriter, r *http.Request) {
	response.OK(w, h.svc.Staff.ListRoles())
}

// GetRolePermissions godoc
//
//	@Summary      Get permissions for a specific role
//	@Description  Returns the default permission list for the named role. Useful for building custom permission selectors.
//	@Tags         admin-staff
//	@Produce      json
//	@Security     BearerAuth
//	@Param        name  path      string  true  "Role name"  Enums(admin, compliance, support, finance, readonly)
//	@Success      200   {object}  map[string]any
//	@Failure      400   {object}  ErrorResponse
//	@Router       /admin/roles/{name} [get]
func (h *AdminStaffHandler) GetRolePermissions(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	perms := services.RolePermissions(name)
	if perms == nil {
		response.BadRequest(w, "INVALID_ROLE", "unknown role: "+name)
		return
	}
	response.OK(w, map[string]any{
		"role":        name,
		"label":       services.RoleLabels[name],
		"permissions": perms,
	})
}

// GetAuditLog godoc
//
//	@Summary      Admin audit trail
//	@Description  Returns paginated log of all admin actions (KYC decisions, user disables, staff changes, FX rate updates). Filterable by staff member, resource type, or specific resource ID.
//	@Tags         admin-staff
//	@Produce      json
//	@Security     BearerAuth
//	@Param        staff_id     query  string  false  "Filter by staff member ID"
//	@Param        resource     query  string  false  "Filter by resource type (kyc, user, staff, withdrawal_rate)"
//	@Param        resource_id  query  string  false  "Filter by specific resource ID"
//	@Param        page         query  int     false  "Page"
//	@Param        limit        query  int     false  "Results per page (max 100)"
//	@Success      200  {object}  map[string]any
//	@Failure      403  {object}  ErrorResponse
//	@Router       /admin/audit-log [get]
func (h *AdminStaffHandler) GetAuditLog(w http.ResponseWriter, r *http.Request) {
	var staffID *uuid.UUID
	if raw := r.URL.Query().Get("staff_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			response.BadRequest(w, "VALIDATION_ERROR", "invalid staff_id")
			return
		}
		staffID = &id
	}

	resource := r.URL.Query().Get("resource")
	resourceID := r.URL.Query().Get("resource_id")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	logs, total, err := h.svc.Staff.ListAuditLogs(r.Context(), staffID, resource, resourceID, int32(page), int32(limit))
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, map[string]any{
		"logs":  logs,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}
