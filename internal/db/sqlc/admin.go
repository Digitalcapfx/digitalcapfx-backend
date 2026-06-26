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
	UserID             *uuid.UUID `json:"user_id"`             // nil until invite accepted
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

type CreateStaffMemberParams struct {
	ID                 uuid.UUID
	Email              string
	Name               string
	Role               string
	CustomPermissions  []string
	RevokedPermissions []string
	InvitedBy          *uuid.UUID
	InviteToken        string
}

type UpdateStaffMemberParams struct {
	ID                 uuid.UUID
	Role               string
	CustomPermissions  []string
	RevokedPermissions []string
}

type ListStaffMembersParams struct {
	IncludeInactive bool
	Limit           int32
	Offset          int32
}

func (q *Queries) CreateStaffMember(ctx context.Context, arg CreateStaffMemberParams) (StaffMember, error) {
	return StaffMember{}, errNotImplemented
}

func (q *Queries) GetStaffMemberByID(ctx context.Context, id uuid.UUID) (StaffMember, error) {
	return StaffMember{}, errNotImplemented
}

func (q *Queries) GetStaffMemberByUserID(ctx context.Context, userID uuid.UUID) (StaffMember, error) {
	return StaffMember{}, errNotImplemented
}

func (q *Queries) GetStaffMemberByEmail(ctx context.Context, email string) (StaffMember, error) {
	return StaffMember{}, errNotImplemented
}

func (q *Queries) GetStaffMemberByInviteToken(ctx context.Context, token string) (StaffMember, error) {
	return StaffMember{}, errNotImplemented
}

func (q *Queries) ListStaffMembers(ctx context.Context, arg ListStaffMembersParams) ([]StaffMember, error) {
	return nil, errNotImplemented
}

func (q *Queries) CountStaffMembers(ctx context.Context, includeInactive bool) (int64, error) {
	return 0, errNotImplemented
}

func (q *Queries) UpdateStaffMember(ctx context.Context, arg UpdateStaffMemberParams) (StaffMember, error) {
	return StaffMember{}, errNotImplemented
}

func (q *Queries) AcceptStaffInvite(ctx context.Context, token string, userID uuid.UUID) error {
	return errNotImplemented
}

func (q *Queries) DisableStaffMember(ctx context.Context, id uuid.UUID) error {
	return errNotImplemented
}

func (q *Queries) EnableStaffMember(ctx context.Context, id uuid.UUID) error {
	return errNotImplemented
}

func (q *Queries) UpdateStaffLastLogin(ctx context.Context, id uuid.UUID) error {
	return errNotImplemented
}

// ─── Audit log ────────────────────────────────────────────────────────────────

type AdminAuditLog struct {
	ID         uuid.UUID       `json:"id"`
	StaffID    uuid.UUID       `json:"staff_id"`
	StaffName  string          `json:"staff_name"`
	StaffEmail string          `json:"staff_email"`
	Action     string          `json:"action"`      // "kyc.approve" | "user.disable" | "staff.invite" | ...
	Resource   string          `json:"resource"`    // "kyc" | "user" | "staff" | "withdrawal_rate"
	ResourceID string          `json:"resource_id"`
	Details    json.RawMessage `json:"details"`
	IPAddress  string          `json:"ip_address"`
	CreatedAt  time.Time       `json:"created_at"`
}

type CreateAdminAuditLogParams struct {
	ID         uuid.UUID
	StaffID    uuid.UUID
	StaffName  string
	StaffEmail string
	Action     string
	Resource   string
	ResourceID string
	Details    json.RawMessage
	IPAddress  string
}

type ListAdminAuditLogsParams struct {
	StaffID    *uuid.UUID
	Resource   string
	ResourceID string
	Limit      int32
	Offset     int32
}

func (q *Queries) CreateAdminAuditLog(ctx context.Context, arg CreateAdminAuditLogParams) (AdminAuditLog, error) {
	return AdminAuditLog{}, errNotImplemented
}

func (q *Queries) ListAdminAuditLogs(ctx context.Context, arg ListAdminAuditLogsParams) ([]AdminAuditLog, error) {
	return nil, errNotImplemented
}

func (q *Queries) CountAdminAuditLogs(ctx context.Context, arg ListAdminAuditLogsParams) (int64, error) {
	return 0, errNotImplemented
}

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

type AdminDashboardStats struct {
	TotalUsers    int64   `json:"total_users"`
	ActiveUsers   int64   `json:"active_users"`
	DisabledUsers int64   `json:"disabled_users"`
	PendingKYC    int64   `json:"pending_kyc"`
	ApprovedKYC   int64   `json:"approved_kyc"`
	RejectedKYC   int64   `json:"rejected_kyc"`
	TotalStaff    int64   `json:"total_staff"`
	ActiveStaff   int64   `json:"active_staff"`
	TxCount30d    int64   `json:"tx_count_30d"`
	TxVolume30d   float64 `json:"tx_volume_30d"`
	NewUsers7d    int64   `json:"new_users_7d"`
	NewUsers30d   int64   `json:"new_users_30d"`
}

func (q *Queries) GetAdminDashboardStats(ctx context.Context) (AdminDashboardStats, error) {
	return AdminDashboardStats{}, errNotImplemented
}

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
