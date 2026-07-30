package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type staffPermCtxKey struct{}

// LoadStaffPermissions is a middleware that runs after Auth. It looks up the
// authenticated user in the admin_staff table, loads their role +
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

			// Owner: trust the JWT role. The bootstrap owner has NO admin_staff row,
			// so StaffID must be empty — otherwise downstream writes would store the
			// owner's user_id in columns that FK-reference admin_staff(id) (invited_by,
			// audit staff_id, updated_by) and violate the constraint. We still pull the
			// owner's name/email so their actions leave a meaningful audit trail.
			if role == "owner" {
				var firstName, lastName, ownerEmail string
				_ = pool.QueryRow(r.Context(),
					`SELECT first_name, last_name, COALESCE(email, '') FROM users WHERE id = $1`,
					userID,
				).Scan(&firstName, &lastName, &ownerEmail)

				set := services.StaffPermissionSet{
					StaffID:    "", // no admin_staff row for the owner
					StaffName:  strings.TrimSpace(firstName + " " + lastName),
					StaffEmail: ownerEmail,
					Role:       "owner",
				}
				ctx := context.WithValue(r.Context(), staffPermCtxKey{}, set)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Non-owner: must have an active admin_staff record.
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
				       COALESCE(custom_permissions, '[]'::jsonb),
				       COALESCE(revoked_permissions, '[]'::jsonb),
				       is_active
				FROM admin_staff
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

// StaffIDFromContext returns the admin_staff.id (not user_id) for audit logging.
func StaffIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	s, ok := ctx.Value(staffPermCtxKey{}).(services.StaffPermissionSet)
	if !ok || s.StaffID == "" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(s.StaffID)
	return id, err == nil
}
