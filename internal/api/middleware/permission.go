package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type staffPermCtxKey struct{}

// LoadStaffPermissions is a middleware that runs after Auth. It looks up the
// authenticated user in the staff_members table, loads their role +
// custom/revoked permissions into the request context, and rejects the request
// with 403 if the user is not a staff member or has been disabled.
//
// Requests from the "owner" role skip the DB look-up (the JWT claim is trusted).
func LoadStaffPermissions(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := UserIDFromContext(r.Context())
			if !ok {
				response.Unauthorized(w, "unauthorized")
				return
			}

			role := RoleFromContext(r.Context())

			// Owner: trust JWT, no DB query required.
			if role == "owner" {
				set := services.StaffPermissionSet{
					StaffID: userID.String(),
					Role:    "owner",
				}
				ctx := context.WithValue(r.Context(), staffPermCtxKey{}, set)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Non-owner: must have an active staff_members record.
			var (
				staffID            string
				staffName          string
				staffEmail         string
				staffRole          string
				customPerms        []string
				revokedPerms       []string
				isActive           bool
			)

			err := pool.QueryRow(r.Context(), `
				SELECT id::text, name, email, role,
				       COALESCE(custom_permissions, '{}'),
				       COALESCE(revoked_permissions, '{}'),
				       is_active
				FROM staff_members
				WHERE user_id = $1
			`, userID).Scan(
				&staffID, &staffName, &staffEmail, &staffRole,
				&customPerms, &revokedPerms, &isActive,
			)
			if err != nil {
				// Not a staff member — might be a regular user trying to hit admin routes.
				response.Forbidden(w, "staff access required")
				return
			}
			if !isActive {
				response.Forbidden(w, "your staff account has been disabled")
				return
			}

			set := services.StaffPermissionSet{
				StaffID:    staffID,
				StaffName:  staffName,
				StaffEmail: staffEmail,
				Role:       staffRole,
				Custom:     customPerms,
				Revoked:    revokedPerms,
			}
			ctx := context.WithValue(r.Context(), staffPermCtxKey{}, set)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequirePermission returns a middleware that rejects the request with 403 if
// the loaded StaffPermissionSet does not include the requested permission.
// Must be placed after LoadStaffPermissions in the middleware chain.
func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			set, ok := r.Context().Value(staffPermCtxKey{}).(services.StaffPermissionSet)
			if !ok {
				response.Forbidden(w, "staff permissions not loaded")
				return
			}
			if !services.HasPermission(set, permission) {
				response.JSON(w, http.StatusForbidden, response.Envelope{
					Success: false,
					Error: &response.Error{
						Code:    "PERMISSION_DENIED",
						Message: "you do not have the '" + permission + "' permission",
					},
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// StaffPermissionsFromContext retrieves the permission set stored by LoadStaffPermissions.
func StaffPermissionsFromContext(ctx context.Context) (services.StaffPermissionSet, bool) {
	s, ok := ctx.Value(staffPermCtxKey{}).(services.StaffPermissionSet)
	return s, ok
}

// StaffIDFromContext returns the staff_members.id (not user_id) for audit logging.
func StaffIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	s, ok := ctx.Value(staffPermCtxKey{}).(services.StaffPermissionSet)
	if !ok || s.StaffID == "" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(s.StaffID)
	return id, err == nil
}
