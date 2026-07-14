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

// adminUserFilterWhere is the shared WHERE clause for the admin user list and
// count. Filters are optional: an empty string / nil disables that filter.
const adminUserFilterWhere = `
	WHERE ($1::text = '' OR u.first_name ILIKE '%'||$1||'%' OR u.last_name ILIKE '%'||$1||'%'
	        OR u.email ILIKE '%'||$1||'%' OR u.phone_number ILIKE '%'||$1||'%')
	  AND ($2::text = '' OR u.kyc_status = $2)
	  AND ($3::bool IS NULL OR u.is_active = $3)
	  AND ($4::text = '' OR u.role = $4)`

func (q *Queries) ListUsersForAdmin(ctx context.Context, arg ListUsersForAdminParams) ([]AdminUserView, error) {
	const sql = `
	SELECT u.id, u.phone_number, u.email, u.first_name, u.last_name, u.kyc_status,
	       u.is_active, u.role, u.created_at,
	       (SELECT count(*) FROM accounts a WHERE a.user_id = u.id)::int AS account_count
	FROM users u` + adminUserFilterWhere + `
	ORDER BY u.created_at DESC
	LIMIT $5 OFFSET $6`

	rows, err := q.db.Query(ctx, sql, arg.Search, arg.KycStatus, arg.IsActive, arg.Role, arg.Limit, arg.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []AdminUserView{}
	for rows.Next() {
		var i AdminUserView
		if err := rows.Scan(
			&i.ID, &i.PhoneNumber, &i.Email, &i.FirstName, &i.LastName, &i.KycStatus,
			&i.IsActive, &i.Role, &i.CreatedAt, &i.AccountCount,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

func (q *Queries) CountUsersForAdmin(ctx context.Context, arg ListUsersForAdminParams) (int64, error) {
	const sql = `SELECT count(*) FROM users u` + adminUserFilterWhere
	var n int64
	err := q.db.QueryRow(ctx, sql, arg.Search, arg.KycStatus, arg.IsActive, arg.Role).Scan(&n)
	return n, err
}

// GetUserForAdmin returns the full user record; it delegates to the existing
// single-user query.
func (q *Queries) GetUserForAdmin(ctx context.Context, id uuid.UUID) (User, error) {
	return q.GetUserByID(ctx, id)
}

func (q *Queries) DisableUser(ctx context.Context, id uuid.UUID) error {
	_, err := q.db.Exec(ctx, `UPDATE users SET is_active = false, updated_at = now() WHERE id = $1`, id)
	return err
}

func (q *Queries) EnableUser(ctx context.Context, id uuid.UUID) error {
	_, err := q.db.Exec(ctx, `UPDATE users SET is_active = true, updated_at = now() WHERE id = $1`, id)
	return err
}

// ResetUserKYC returns the user to an unverified state and hands the decision
// back to the automated provider (clears any admin manual override).
func (q *Queries) ResetUserKYC(ctx context.Context, id uuid.UUID) error {
	_, err := q.db.Exec(ctx, `UPDATE users SET kyc_status = 'pending', kyc_manual_override = false, updated_at = now() WHERE id = $1`, id)
	return err
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
	const sql = `
	SELECT id, reference, account_id, type, amount, currency, fee, description, status, metadata, created_at, updated_at
	FROM transactions
	WHERE account_id IN (SELECT id FROM accounts WHERE user_id = $1)
	ORDER BY created_at DESC
	LIMIT $2 OFFSET $3`
	rows, err := q.db.Query(ctx, sql, arg.UserID, arg.Limit, arg.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Transaction{}
	for rows.Next() {
		var i Transaction
		if err := rows.Scan(
			&i.ID, &i.Reference, &i.AccountID, &i.Type, &i.Amount, &i.Currency,
			&i.Fee, &i.Description, &i.Status, &i.Metadata, &i.CreatedAt, &i.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

func (q *Queries) CountTransactionsByUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	const sql = `SELECT count(*) FROM transactions WHERE account_id IN (SELECT id FROM accounts WHERE user_id = $1)`
	var n int64
	err := q.db.QueryRow(ctx, sql, userID).Scan(&n)
	return n, err
}
