package db

import (
	"context"
	"encoding/json"

	"time"

	"github.com/google/uuid"
)

// ─── Staff members ────────────────────────────────────────────────────────────

type StaffMember struct {
	ID                 uuid.UUID  `json:"id"`
	UserID             *uuid.UUID `json:"user_id"` // nil until invite accepted
	Email              string     `json:"email"`
	Name               string     `json:"name"`
	Role               string     `json:"role"`                // "owner"|"admin"|"compliance"|"support"|"finance"|"readonly"
	CustomPermissions  []string   `json:"custom_permissions"`  // grants beyond role defaults
	RevokedPermissions []string   `json:"revoked_permissions"` // revoked from role defaults
	IsActive           bool       `json:"is_active"`
	InvitedBy          *uuid.UUID `json:"invited_by"`
	InviteToken        *string    `json:"-"`
	InviteAcceptedAt   *time.Time `json:"invite_accepted_at"`
	LastLoginAt        *time.Time `json:"last_login_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// FromAdminStaff converts a generated AdminStaff row into the StaffMember view,
// decoding the JSONB permission arrays into string slices.
func FromAdminStaff(m AdminStaff) StaffMember {
	var custom, revoked []string
	_ = json.Unmarshal(m.CustomPermissions, &custom)
	_ = json.Unmarshal(m.RevokedPermissions, &revoked)
	return StaffMember{
		ID:                 m.ID,
		UserID:             m.UserID,
		Email:              m.Email,
		Name:               m.Name,
		Role:               m.Role,
		CustomPermissions:  custom,
		RevokedPermissions: revoked,
		IsActive:           m.IsActive,
		InvitedBy:          m.InvitedBy,
		InviteToken:        m.InviteToken,
		InviteAcceptedAt:   m.InviteAcceptedAt,
		LastLoginAt:        m.LastLoginAt,
		CreatedAt:          m.CreatedAt,
		UpdatedAt:          m.UpdatedAt,
	}
}

// ─── Audit log ────────────────────────────────────────────────────────────────

// ─── User management (admin view) ────────────────────────────────────────────

type AdminUserView struct {
	ID           uuid.UUID  `json:"id"`
	PhoneNumber  string     `json:"phone_number"`
	Email        *string    `json:"email"`
	FirstName    string     `json:"first_name"`
	LastName     string     `json:"last_name"`
	KycStatus    string     `json:"kyc_status"`
	IsActive     bool       `json:"is_active"`
	Role         string     `json:"role"`
	AccountCount int        `json:"account_count"`
	CreatedAt    time.Time  `json:"created_at"`
	LastLoginAt  *time.Time `json:"last_login_at"`
}

type ListUsersForAdminParams struct {
	Search    string // name, email, phone
	KycStatus string // "" | "pending" | "approved" | "rejected" | "under_review"
	IsActive  *bool  // nil = all, true = active, false = disabled
	Role      string // "" | "user" | "admin"
	Limit     int32
	Offset    int32
}

func (q *Queries) ListUsersForAdmin(ctx context.Context, arg ListUsersForAdminParams) ([]AdminUserView, error) {
	return nil, errNotImplemented
}

func (q *Queries) CountUsersForAdmin(ctx context.Context, arg ListUsersForAdminParams) (int64, error) {
	return 0, errNotImplemented
}

func (q *Queries) GetUserForAdmin(ctx context.Context, id uuid.UUID) (User, error) {
	return User{}, errNotImplemented
}

func (q *Queries) DisableUser(ctx context.Context, id uuid.UUID) error {
	return errNotImplemented
}

func (q *Queries) EnableUser(ctx context.Context, id uuid.UUID) error {
	return errNotImplemented
}

func (q *Queries) ResetUserKYC(ctx context.Context, id uuid.UUID) error {
	return errNotImplemented
}

// ─── Admin dashboard stats ────────────────────────────────────────────────────

type AdminDashboardStats = GetAdminDashboardStatsRow

// ─── Admin transaction queries ────────────────────────────────────────────────

type ListTransactionsByUserParams struct {
	UserID uuid.UUID
	Limit  int32
	Offset int32
}

func (q *Queries) ListTransactionsByUser(ctx context.Context, arg ListTransactionsByUserParams) ([]Transaction, error) {
	return nil, errNotImplemented
}

func (q *Queries) CountTransactionsByUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	return 0, errNotImplemented
}
