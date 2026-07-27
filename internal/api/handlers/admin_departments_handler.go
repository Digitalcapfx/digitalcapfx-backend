package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

// AdminDepartmentsHandler is CRUD for admin team departments.
type AdminDepartmentsHandler struct {
	svc *services.Services
}

func NewAdminDepartmentsHandler(svc *services.Services) *AdminDepartmentsHandler {
	return &AdminDepartmentsHandler{svc: svc}
}

// ListDepartments godoc
//
//	@Summary      List departments
//	@Description  Returns all admin team departments with the number of staff assigned to each.
//	@Tags         admin-departments
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  map[string]any
//	@Failure      403  {object}  ErrorResponse
//	@Router       /admin/departments [get]
func (h *AdminDepartmentsHandler) ListDepartments(w http.ResponseWriter, r *http.Request) {
	deps, err := h.svc.Department.List(r.Context())
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, map[string]any{"departments": deps})
}

// CreateDepartment godoc
//
//	@Summary      Create a department
//	@Tags         admin-departments
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      DepartmentRequest  true  "Department"
//	@Success      201   {object}  db.AdminDepartment
//	@Failure      400   {object}  ErrorResponse
//	@Failure      409   {object}  ErrorResponse
//	@Router       /admin/departments [post]
func (h *AdminDepartmentsHandler) CreateDepartment(w http.ResponseWriter, r *http.Request) {
	set, _ := middleware.StaffPermissionsFromContext(r.Context())
	var body DepartmentRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "name is required")
		return
	}
	dep, err := h.svc.Department.Create(r.Context(), body.Name, body.Description)
	if err != nil {
		if errors.Is(err, services.ErrDepartmentExists) {
			response.Conflict(w, "DEPARTMENT_EXISTS", err.Error())
			return
		}
		response.BadRequest(w, "VALIDATION_ERROR", err.Error())
		return
	}
	h.svc.Staff.LogAction(r.Context(), set, "department.create", "departments", dep.ID.String(), map[string]any{"name": dep.Name}, r.RemoteAddr)
	response.Created(w, dep)
}

// GetDepartment godoc
//
//	@Summary      Get a department
//	@Tags         admin-departments
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id  path  string  true  "Department ID"
//	@Success      200  {object}  db.AdminDepartment
//	@Failure      404  {object}  ErrorResponse
//	@Router       /admin/departments/{id} [get]
func (h *AdminDepartmentsHandler) GetDepartment(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid department id")
		return
	}
	dep, err := h.svc.Department.Get(r.Context(), id)
	if err != nil {
		response.NotFound(w, "department not found")
		return
	}
	response.OK(w, dep)
}

// UpdateDepartment godoc
//
//	@Summary      Update a department
//	@Tags         admin-departments
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      string             true  "Department ID"
//	@Param        body  body      DepartmentRequest  true  "Fields to update"
//	@Success      200   {object}  db.AdminDepartment
//	@Failure      404   {object}  ErrorResponse
//	@Failure      409   {object}  ErrorResponse
//	@Router       /admin/departments/{id} [patch]
func (h *AdminDepartmentsHandler) UpdateDepartment(w http.ResponseWriter, r *http.Request) {
	set, _ := middleware.StaffPermissionsFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid department id")
		return
	}
	var body DepartmentRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	var namePtr, descPtr *string
	if strings.TrimSpace(body.Name) != "" {
		n := strings.TrimSpace(body.Name)
		namePtr = &n
	}
	if body.Description != "" {
		d := strings.TrimSpace(body.Description)
		descPtr = &d
	}
	dep, err := h.svc.Department.Update(r.Context(), id, namePtr, descPtr)
	switch {
	case err == nil:
	case errors.Is(err, services.ErrDepartmentNotFound):
		response.NotFound(w, err.Error())
		return
	case errors.Is(err, services.ErrDepartmentExists):
		response.Conflict(w, "DEPARTMENT_EXISTS", err.Error())
		return
	default:
		response.BadRequest(w, "VALIDATION_ERROR", err.Error())
		return
	}
	h.svc.Staff.LogAction(r.Context(), set, "department.update", "departments", id.String(), nil, r.RemoteAddr)
	response.OK(w, dep)
}

// DeleteDepartment godoc
//
//	@Summary      Delete a department
//	@Description  Deletes a department. Fails if staff are still assigned to it — reassign them first.
//	@Tags         admin-departments
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id  path  string  true  "Department ID"
//	@Success      200  {object}  MessageResponse
//	@Failure      404  {object}  ErrorResponse
//	@Failure      409  {object}  ErrorResponse
//	@Router       /admin/departments/{id} [delete]
func (h *AdminDepartmentsHandler) DeleteDepartment(w http.ResponseWriter, r *http.Request) {
	set, _ := middleware.StaffPermissionsFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid department id")
		return
	}
	switch err := h.svc.Department.Delete(r.Context(), id); {
	case err == nil:
	case errors.Is(err, services.ErrDepartmentNotFound):
		response.NotFound(w, err.Error())
		return
	case errors.Is(err, services.ErrDepartmentInUse):
		response.Conflict(w, "DEPARTMENT_IN_USE", err.Error())
		return
	default:
		response.InternalError(w)
		return
	}
	h.svc.Staff.LogAction(r.Context(), set, "department.delete", "departments", id.String(), nil, r.RemoteAddr)
	response.OKWithMessage(w, "department deleted", nil)
}
