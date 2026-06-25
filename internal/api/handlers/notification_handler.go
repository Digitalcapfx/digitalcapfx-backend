package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type NotificationHandler struct {
	svc *services.Services
}

func NewNotificationHandler(svc *services.Services) *NotificationHandler {
	return &NotificationHandler{svc: svc}
}

// ListNotifications godoc
//
//	@Summary      List notifications
//	@Description  Returns paginated notifications for the authenticated user. Pass ?unread=true to filter unread only. Response includes an unread badge count.
//	@Tags         notifications
//	@Produce      json
//	@Security     BearerAuth
//	@Param        page        query  int   false  "Page number (default 1)"
//	@Param        per_page    query  int   false  "Items per page (default 20, max 50)"
//	@Param        unread      query  bool  false  "Return only unread notifications"
//	@Success      200  {object}  object
//	@Failure      401  {object}  ErrorResponse
//	@Router       /notifications [get]
func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	page := int32(1)
	limit := int32(20)
	unreadOnly := false

	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = int32(n)
		}
	}
	if v := r.URL.Query().Get("per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 50 {
			limit = int32(n)
		}
	}
	if r.URL.Query().Get("unread") == "true" {
		unreadOnly = true
	}

	result, err := h.svc.Notifications.List(r.Context(), userID, page, limit, unreadOnly)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, result)
}

// UnreadCount godoc
//
//	@Summary      Unread notification count
//	@Description  Returns the number of unread notifications. Use for the badge dot on the bell icon.
//	@Tags         notifications
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  object
//	@Failure      401  {object}  ErrorResponse
//	@Router       /notifications/unread-count [get]
func (h *NotificationHandler) UnreadCount(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	count, err := h.svc.Notifications.UnreadCount(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, map[string]int64{"unread": count})
}

// MarkRead godoc
//
//	@Summary      Mark notification as read
//	@Description  Marks a single notification as read. Only the owning user can mark their own notifications.
//	@Tags         notifications
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      string  true  "Notification ID"
//	@Success      200  {object}  object
//	@Failure      400  {object}  ErrorResponse
//	@Failure      401  {object}  ErrorResponse
//	@Router       /notifications/{id}/read [patch]
func (h *NotificationHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid notification id")
		return
	}

	n, err := h.svc.Notifications.MarkRead(r.Context(), id, userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, n)
}

// MarkAllRead godoc
//
//	@Summary      Mark all notifications as read
//	@Description  Marks every unread notification for the authenticated user as read.
//	@Tags         notifications
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  MessageResponse
//	@Failure      401  {object}  ErrorResponse
//	@Router       /notifications/read-all [patch]
func (h *NotificationHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	if err := h.svc.Notifications.MarkAllRead(r.Context(), userID); err != nil {
		response.InternalError(w)
		return
	}

	response.OKWithMessage(w, "all notifications marked as read", nil)
}
