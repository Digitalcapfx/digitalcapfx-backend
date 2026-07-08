package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type TeamHandler struct {
	svc *services.Services
}

func NewTeamHandler(svc *services.Services) *TeamHandler {
	return &TeamHandler{svc: svc}
}

// GetRolesPermissions godoc
//
//	@Summary      Get available roles and permissions
//	@Description  Returns all available roles and the permissions they carry.
//	@Tags         team
//	@Produce      json
//	@Success      200  {object}  RolesPermissionsResponse
//	@Router       /team/roles-permissions [get]
func (h *TeamHandler) GetRolesPermissions(w http.ResponseWriter, r *http.Request) {
	roles := []map[string]any{
		{
			"role":        "manager",
			"permissions": []string{"team:view", "team:invite", "team:manage_permissions"},
		},
		{
			"role":        "developer",
			"permissions": []string{"team:view"},
		},
		{
			"role":        "viewer",
			"permissions": []string{"team:view"},
		},
	}
	response.OK(w, map[string]any{"roles": roles})
}

// ListMembers godoc
//
//	@Summary      List team members
//	@Description  Returns all staff members invited or active for this business.
//	@Tags         team
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {array}   TeamMemberResponse
//	@Failure      401  {object}  ErrorResponse
//	@Router       /team [get]
func (h *TeamHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	businessUserID, ok := middleware.BusinessUserIDFromContext(r.Context())
	if !ok {
		// Fall back to the authenticated userID if no separate business context
		uid, uok := middleware.UserIDFromContext(r.Context())
		if !uok {
			response.Unauthorized(w, "unauthorized")
			return
		}
		businessUserID = uid
	}

	members, err := h.svc.Business.ListMerchantStaff(r.Context(), businessUserID)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, members)
}

// InviteMember godoc
//
//	@Summary      Invite a team member
//	@Description  Sends an invite to a new staff member for this business account.
//	@Tags         team
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      InviteTeamMemberRequest  true  "Invite details"
//	@Success      201   {object}  TeamInviteResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      409   {object}  ErrorResponse
//	@Router       /team/invite [post]
func (h *TeamHandler) InviteMember(w http.ResponseWriter, r *http.Request) {
	businessUserID, ok := getBusinessOrUserID(r)
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON payload")
		return
	}

	staff, token, err := h.svc.Business.InviteMerchantStaff(r.Context(), businessUserID, body.Email, body.Role)
	if errors.Is(err, services.ErrInvalidMerchantRole) {
		response.BadRequest(w, "INVALID_ROLE", err.Error())
		return
	}
	if errors.Is(err, services.ErrMerchantStaffAlreadyExists) {
		response.Conflict(w, "STAFF_EXISTS", err.Error())
		return
	}
	if err != nil {
		response.InternalError(w)
		return
	}

	response.Created(w, map[string]any{
		"staff":        staff,
		"invite_token": token,
	})
}

// AcceptInvite godoc
//
//	@Summary      Accept a team invite
//	@Description  Links an authenticated user to a pending staff invite using the invite token.
//	@Tags         team
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      AcceptInviteRequest  true  "Invite token"
//	@Success      200   {object}  MessageResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Router       /team/accept-invite [post]
func (h *TeamHandler) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body struct {
		InviteToken string `json:"invite_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.InviteToken == "" {
		response.BadRequest(w, "INVALID_BODY", "invite_token is required")
		return
	}

	if err := h.svc.Business.AcceptMerchantStaffInvite(r.Context(), body.InviteToken, userID); err != nil {
		if errors.Is(err, services.ErrInvalidMerchantInviteToken) {
			response.BadRequest(w, "INVALID_TOKEN", err.Error())
			return
		}
		response.InternalError(w)
		return
	}

	response.OKWithMessage(w, "invite accepted", nil)
}

// UpdateMemberRole godoc
//
//	@Summary      Update a team member's role
//	@Description  Updates the role of an existing staff member within the business account.
//	@Tags         team
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      string               true  "Staff member ID"
//	@Param        body  body      UpdateRoleRequest    true  "New role"
//	@Success      200   {object}  MessageResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      404   {object}  ErrorResponse
//	@Router       /team/{id}/role [put]
func (h *TeamHandler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	businessUserID, ok := getBusinessOrUserID(r)
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	idStr := chi.URLParam(r, "id")
	staffID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "invalid staff member ID")
		return
	}

	var body struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON payload")
		return
	}

	if err := h.svc.Business.UpdateMerchantStaffRole(r.Context(), staffID, businessUserID, body.Role); err != nil {
		if errors.Is(err, services.ErrInvalidMerchantRole) {
			response.BadRequest(w, "INVALID_ROLE", err.Error())
			return
		}
		if errors.Is(err, services.ErrMerchantStaffNotFound) {
			response.NotFound(w, "staff member not found")
			return
		}
		response.InternalError(w)
		return
	}

	response.OKWithMessage(w, "role updated", nil)
}

// RemoveMember godoc
//
//	@Summary      Remove a team member
//	@Description  Removes a staff member from the business account.
//	@Tags         team
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id  path      string  true  "Staff member ID"
//	@Success      204
//	@Failure      400  {object}  ErrorResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      404  {object}  ErrorResponse
//	@Router       /team/{id} [delete]
func (h *TeamHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	businessUserID, ok := getBusinessOrUserID(r)
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	idStr := chi.URLParam(r, "id")
	staffID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "invalid staff member ID")
		return
	}

	if err := h.svc.Business.RemoveMerchantStaff(r.Context(), staffID, businessUserID); err != nil {
		if errors.Is(err, services.ErrMerchantStaffNotFound) {
			response.NotFound(w, "staff member not found")
			return
		}
		response.InternalError(w)
		return
	}

	response.NoContent(w)
}

// helper: get business user ID from context, falling back to the regular user ID
func getBusinessOrUserID(r *http.Request) (uuid.UUID, bool) {
	if id, ok := middleware.BusinessUserIDFromContext(r.Context()); ok {
		return id, true
	}
	return middleware.UserIDFromContext(r.Context())
}