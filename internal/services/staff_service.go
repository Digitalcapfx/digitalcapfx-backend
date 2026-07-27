package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
	"github.com/rachfinance/digitalfx/internal/pkg/email"
	"github.com/rachfinance/digitalfx/internal/pkg/hash"
)

var (
	ErrStaffNotFound      = errors.New("staff member not found")
	ErrStaffAlreadyExists = errors.New("a staff member with that email already exists")
	ErrCannotModifyOwner  = errors.New("the owner account cannot be modified via this endpoint")
	ErrInvalidRole        = errors.New("invalid role — must be one of: admin, compliance, support, finance, readonly")
	ErrInvalidInviteToken = errors.New("invite token is invalid or has already been used")
	ErrInviteOTPInvalid   = errors.New("invite code is invalid or has expired")
	ErrInviteAlreadyUsed  = errors.New("this invite has already been accepted")
	ErrNoPendingInvite    = errors.New("no pending invite found (already accepted or does not exist)")
)

// ─── Response types ───────────────────────────────────────────────────────────

type StaffMemberView struct {
	ID                  string     `json:"id"`
	Email               string     `json:"email"`
	Name                string     `json:"name"`
	Role                string     `json:"role"`
	RoleLabel           string     `json:"role_label"`
	RoleDescription     string     `json:"role_description"`
	EffectivePermissions []string  `json:"effective_permissions"`
	CustomPermissions   []string   `json:"custom_permissions"`
	RevokedPermissions  []string   `json:"revoked_permissions"`
	IsActive            bool       `json:"is_active"`
	InviteAccepted      bool       `json:"invite_accepted"`
	DepartmentID        *string    `json:"department_id"`
	LastLoginAt         *time.Time `json:"last_login_at"`
	CreatedAt           time.Time  `json:"created_at"`
}

type StaffListResult struct {
	Staff []StaffMemberView `json:"staff"`
	Total int64             `json:"total"`
	Page  int32             `json:"page"`
	Limit int32             `json:"limit"`
}

type RoleView struct {
	Role        string   `json:"role"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	Assignable  bool     `json:"assignable"` // owner is not assignable via invite
}

type InviteStaffInput struct {
	InviterStaffID uuid.UUID
	Email          string
	Name           string
	Role           string
	DepartmentID   *uuid.UUID
	CustomPerms    []string
	RevokedPerms   []string
}

type UpdateStaffInput struct {
	Role         string
	CustomPerms  []string
	RevokedPerms []string
}

// ─── Service ──────────────────────────────────────────────────────────────────

type StaffService struct {
	pool        *pgxpool.Pool
	emailClient *email.Client
	baseURL     string
	logger      *zap.Logger
}

func NewStaffService(pool *pgxpool.Pool, emailClient *email.Client, baseURL string, logger *zap.Logger) *StaffService {
	return &StaffService{pool: pool, emailClient: emailClient, baseURL: baseURL, logger: logger}
}

// InviteStaff creates a pending staff member record and sends an invitation email.
// Returns the new StaffMember (invite not yet accepted) and the raw token for testing/dev.
func (s *StaffService) InviteStaff(ctx context.Context, in InviteStaffInput) (*StaffMemberView, error) {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Role = strings.ToLower(strings.TrimSpace(in.Role))

	if !IsValidRole(in.Role) {
		return nil, ErrInvalidRole
	}
	if in.Email == "" || in.Name == "" {
		return nil, errors.New("email and name are required")
	}

	q := db.New(s.pool)

	// Duplicate check.
	if _, err := q.GetStaffMemberByEmail(ctx, in.Email); err == nil {
		return nil, ErrStaffAlreadyExists
	}

	// Validate custom/revoked permissions.
	for _, p := range append(in.CustomPerms, in.RevokedPerms...) {
		if !IsValidPermission(p) {
			return nil, fmt.Errorf("unknown permission: %q", p)
		}
	}

	// Validate the department exists, if one was chosen.
	if in.DepartmentID != nil {
		if _, err := q.GetDepartmentByID(ctx, *in.DepartmentID); err != nil {
			return nil, ErrDepartmentNotFound
		}
	}

	token := generateStaffInviteToken()
	otp := generateStaffInviteOTP()
	otpHash, err := hash.PIN(otp)
	if err != nil {
		return nil, fmt.Errorf("hash invite otp: %w", err)
	}
	expires := time.Now().Add(inviteOTPTTL)

	inviterID := in.InviterStaffID
	member, err := q.CreateStaffMember(ctx, db.CreateStaffMemberParams{
		Email:              in.Email,
		Name:               in.Name,
		Role:               in.Role,
		CustomPermissions:  permsJSON(in.CustomPerms),
		RevokedPermissions: permsJSON(in.RevokedPerms),
		InvitedBy:          &inviterID,
		InviteToken:        &token,
		DepartmentID:       in.DepartmentID,
		InviteOtpHash:      &otpHash,
		InviteOtpExpiresAt: &expires,
	})
	if err != nil {
		return nil, fmt.Errorf("create staff: %w", err)
	}

	s.deliverInvite(in.Email, in.Name, in.Role, token, otp)

	s.logger.Info("staff invite created",
		zap.String("email", in.Email),
		zap.String("role", in.Role),
		zap.String("invited_by", in.InviterStaffID.String()),
	)

	return staffToView(db.FromAdminStaff(member)), nil
}

// AcceptInvite links the authenticated user to a pending staff record by
// verifying the one-time code (OTP) emailed to that address. Called after the
// invitee has registered/logged in as a user.
func (s *StaffService) AcceptInvite(ctx context.Context, email, otp string, userID uuid.UUID) error {
	email = strings.ToLower(strings.TrimSpace(email))
	otp = strings.TrimSpace(otp)
	if email == "" || otp == "" {
		return ErrInviteOTPInvalid
	}

	q := db.New(s.pool)
	m, err := q.GetStaffMemberByEmail(ctx, email)
	if err != nil {
		return ErrInviteOTPInvalid
	}
	if m.InviteAcceptedAt != nil {
		return ErrInviteAlreadyUsed
	}
	if m.InviteOtpHash == nil || m.InviteOtpExpiresAt == nil {
		return ErrInviteOTPInvalid
	}
	if time.Now().After(*m.InviteOtpExpiresAt) {
		return ErrInviteOTPInvalid
	}
	if !hash.CheckPIN(*m.InviteOtpHash, otp) {
		return ErrInviteOTPInvalid
	}

	if err := q.AcceptStaffInviteByID(ctx, db.AcceptStaffInviteByIDParams{UserID: &userID, ID: m.ID}); err != nil {
		return ErrInviteOTPInvalid
	}
	s.logger.Info("staff invite accepted", zap.String("email", email), zap.String("user_id", userID.String()))
	return nil
}

// ResendInvite regenerates the invite code + token for a still-pending invite
// and re-sends the email.
func (s *StaffService) ResendInvite(ctx context.Context, id uuid.UUID) error {
	q := db.New(s.pool)
	m, err := q.GetStaffMemberByID(ctx, id)
	if err != nil {
		return ErrStaffNotFound
	}
	if m.InviteAcceptedAt != nil {
		return ErrInviteAlreadyUsed
	}

	token := generateStaffInviteToken()
	otp := generateStaffInviteOTP()
	otpHash, err := hash.PIN(otp)
	if err != nil {
		return fmt.Errorf("hash invite otp: %w", err)
	}
	expires := time.Now().Add(inviteOTPTTL)
	if err := q.SetStaffInviteOTP(ctx, db.SetStaffInviteOTPParams{
		ID:                 id,
		InviteOtpHash:      &otpHash,
		InviteOtpExpiresAt: &expires,
		InviteToken:        &token,
	}); err != nil {
		return fmt.Errorf("resend invite: %w", err)
	}
	s.deliverInvite(m.Email, m.Name, m.Role, token, otp)
	s.logger.Info("staff invite resent", zap.String("email", m.Email))
	return nil
}

// RevokeInvite cancels a still-pending invite (deletes the record, freeing the
// email for a fresh invite). Errors if the invite was already accepted.
func (s *StaffService) RevokeInvite(ctx context.Context, id uuid.UUID) error {
	q := db.New(s.pool)
	rows, err := q.RevokeStaffInvite(ctx, id)
	if err != nil {
		return fmt.Errorf("revoke invite: %w", err)
	}
	if rows == 0 {
		return ErrNoPendingInvite
	}
	return nil
}

// Remove permanently deletes a staff member (any status). The owner cannot be
// removed. The linked user account is untouched — only their admin access is.
func (s *StaffService) Remove(ctx context.Context, id uuid.UUID) error {
	q := db.New(s.pool)
	m, err := q.GetStaffMemberByID(ctx, id)
	if err != nil {
		return ErrStaffNotFound
	}
	if m.Role == "owner" {
		return ErrCannotModifyOwner
	}
	rows, err := q.DeleteStaffMember(ctx, id)
	if err != nil {
		return fmt.Errorf("remove staff: %w", err)
	}
	if rows == 0 {
		return ErrStaffNotFound
	}
	return nil
}

// SetDepartment assigns (or clears, with nil) a staff member's department.
func (s *StaffService) SetDepartment(ctx context.Context, id uuid.UUID, deptID *uuid.UUID) (*StaffMemberView, error) {
	q := db.New(s.pool)
	if _, err := q.GetStaffMemberByID(ctx, id); err != nil {
		return nil, ErrStaffNotFound
	}
	if deptID != nil {
		if _, err := q.GetDepartmentByID(ctx, *deptID); err != nil {
			return nil, ErrDepartmentNotFound
		}
	}
	if err := q.SetStaffDepartment(ctx, db.SetStaffDepartmentParams{ID: id, DepartmentID: deptID}); err != nil {
		return nil, fmt.Errorf("set department: %w", err)
	}
	updated, err := q.GetStaffMemberByID(ctx, id)
	if err != nil {
		return nil, ErrStaffNotFound
	}
	return staffToView(db.FromAdminStaff(updated)), nil
}

// GetByID returns a single staff member.
func (s *StaffService) GetByID(ctx context.Context, id uuid.UUID) (*StaffMemberView, error) {
	q := db.New(s.pool)
	m, err := q.GetStaffMemberByID(ctx, id)
	if err != nil {
		return nil, ErrStaffNotFound
	}
	return staffToView(db.FromAdminStaff(m)), nil
}

// GetByUserID looks up a staff member by their linked user_id (used in auth flow).
func (s *StaffService) GetByUserID(ctx context.Context, userID uuid.UUID) (*StaffMemberView, error) {
	q := db.New(s.pool)
	m, err := q.GetStaffMemberByUserID(ctx, &userID)
	if err != nil {
		return nil, ErrStaffNotFound
	}
	return staffToView(db.FromAdminStaff(m)), nil
}

// List returns paginated staff members.
func (s *StaffService) List(ctx context.Context, includeInactive bool, page, limit int32) (*StaffListResult, error) {
	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	q := db.New(s.pool)
	members, _ := q.ListStaffMembers(ctx, db.ListStaffMembersParams{
		IncludeInactive: includeInactive,
		Limit:           limit,
		Offset:          offset,
	})
	total, _ := q.CountStaffMembers(ctx, includeInactive)

	views := make([]StaffMemberView, 0, len(members))
	for _, m := range members {
		views = append(views, *staffToView(db.FromAdminStaff(m)))
	}
	return &StaffListResult{Staff: views, Total: total, Page: page, Limit: limit}, nil
}

// Update changes a staff member's role and/or custom permissions.
// The owner's role cannot be changed via this endpoint.
func (s *StaffService) Update(ctx context.Context, id uuid.UUID, in UpdateStaffInput) (*StaffMemberView, error) {
	in.Role = strings.ToLower(strings.TrimSpace(in.Role))

	q := db.New(s.pool)
	existing, err := q.GetStaffMemberByID(ctx, id)
	if err != nil {
		return nil, ErrStaffNotFound
	}
	if existing.Role == "owner" {
		return nil, ErrCannotModifyOwner
	}
	if in.Role != "" && !IsValidRole(in.Role) {
		return nil, ErrInvalidRole
	}

	role := existing.Role
	if in.Role != "" {
		role = in.Role
	}
	existingView := db.FromAdminStaff(existing)
	customPerms := in.CustomPerms
	if customPerms == nil {
		customPerms = existingView.CustomPermissions
	}
	revokedPerms := in.RevokedPerms
	if revokedPerms == nil {
		revokedPerms = existingView.RevokedPermissions
	}

	for _, p := range append(customPerms, revokedPerms...) {
		if !IsValidPermission(p) {
			return nil, fmt.Errorf("unknown permission: %q", p)
		}
	}

	updated, err := q.UpdateStaffMember(ctx, db.UpdateStaffMemberParams{
		ID:                 id,
		Role:               &role,
		CustomPermissions:  permsJSON(customPerms),
		RevokedPermissions: permsJSON(revokedPerms),
	})
	if err != nil {
		return nil, fmt.Errorf("update staff: %w", err)
	}
	return staffToView(db.FromAdminStaff(updated)), nil
}

// Disable deactivates a staff member.
func (s *StaffService) Disable(ctx context.Context, id uuid.UUID) error {
	q := db.New(s.pool)
	existing, err := q.GetStaffMemberByID(ctx, id)
	if err != nil {
		return ErrStaffNotFound
	}
	if existing.Role == "owner" {
		return ErrCannotModifyOwner
	}
	return q.DisableStaffMember(ctx, id)
}

// Enable re-activates a previously disabled staff member.
func (s *StaffService) Enable(ctx context.Context, id uuid.UUID) error {
	q := db.New(s.pool)
	if _, err := q.GetStaffMemberByID(ctx, id); err != nil {
		return ErrStaffNotFound
	}
	return q.EnableStaffMember(ctx, id)
}

// ListRoles returns the full roles catalogue with their default permissions.
func (s *StaffService) ListRoles() []RoleView {
	roles := make([]RoleView, 0, len(ValidRoles))
	for _, r := range ValidRoles {
		roles = append(roles, RoleView{
			Role:        r,
			Label:       RoleLabels[r],
			Description: RoleDescriptions[r],
			Permissions: RolePermissions(r),
			Assignable:  r != "owner",
		})
	}
	return roles
}

// LogAction writes an entry to the admin_audit_logs table. Fire-and-forget;
// errors are logged but not returned to the caller.
func (s *StaffService) LogAction(
	ctx context.Context,
	set StaffPermissionSet,
	action, resource, resourceID string,
	details map[string]any,
	ipAddress string,
) {
	raw, _ := json.Marshal(details)
	q := db.New(s.pool)
	_, err := q.CreateAdminAuditLog(ctx, db.CreateAdminAuditLogParams{
		StaffID:    mustParseUUID(set.StaffID),
		StaffName:  set.StaffName,
		StaffEmail: set.StaffEmail,
		Action:     action,
		Resource:   resource,
		ResourceID: ptrString(resourceID),
		Details:    raw,
		IPAddress:  ptrString(ipAddress),
	})
	if err != nil {
		s.logger.Warn("audit log write failed", zap.Error(err))
	}
}

// ListAuditLogs returns paginated admin audit logs with optional filters.
func (s *StaffService) ListAuditLogs(
	ctx context.Context,
	staffID *uuid.UUID,
	resource, resourceID string,
	page, limit int32,
) ([]db.AdminAuditLog, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	q := db.New(s.pool)
	params := db.ListAdminAuditLogsParams{
		StaffID:    staffID,
		Resource:   resource,
		ResourceID: resourceID,
		Limit:      limit,
		Offset:     offset,
	}
	logs, _ := q.ListAdminAuditLogs(ctx, params)
	total, _ := q.CountAdminAuditLogs(ctx, db.CountAdminAuditLogsParams{
		StaffID:    staffID,
		Resource:   resource,
		ResourceID: resourceID,
	})
	return logs, total, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// permsJSON encodes a permission list as JSONB for the admin_staff columns.
// nil becomes an empty JSON array rather than SQL NULL.
func permsJSON(perms []string) []byte {
	if perms == nil {
		perms = []string{}
	}
	b, _ := json.Marshal(perms)
	return b
}

func staffToView(m db.StaffMember) *StaffMemberView {
	set := StaffPermissionSet{
		Role:    m.Role,
		Custom:  m.CustomPermissions,
		Revoked: m.RevokedPermissions,
	}
	return &StaffMemberView{
		ID:                   m.ID.String(),
		Email:                m.Email,
		Name:                 m.Name,
		Role:                 m.Role,
		RoleLabel:            RoleLabels[m.Role],
		RoleDescription:      RoleDescriptions[m.Role],
		EffectivePermissions: EffectivePermissions(set),
		CustomPermissions:    m.CustomPermissions,
		RevokedPermissions:   m.RevokedPermissions,
		IsActive:             m.IsActive,
		InviteAccepted:       m.InviteAcceptedAt != nil,
		DepartmentID:         uuidPtrToStr(m.DepartmentID),
		LastLoginAt:          m.LastLoginAt,
		CreatedAt:            m.CreatedAt,
	}
}

// uuidPtrToStr renders a *uuid.UUID as *string (nil-safe) for JSON responses.
func uuidPtrToStr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

// inviteOTPTTL is how long a staff invite code stays valid (matches email copy).
const inviteOTPTTL = 7 * 24 * time.Hour

func generateStaffInviteToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return uuid.New().String() // fallback
	}
	return hex.EncodeToString(b)
}

// generateStaffInviteOTP returns a random 6-digit numeric one-time code.
func generateStaffInviteOTP() string {
	const digits = "0123456789"
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "000000"
	}
	for i := range b {
		b[i] = digits[int(b[i])%len(digits)]
	}
	return string(b)
}

func mustParseUUID(s string) uuid.UUID {
	id, _ := uuid.Parse(s)
	return id
}

// deliverInvite emails the invite (OTP + accept link). When no email client is
// configured (dev), it logs the OTP so the flow can still be completed.
func (s *StaffService) deliverInvite(toEmail, name, role, inviteToken, otp string) {
	if s.emailClient == nil {
		s.logger.Warn("staff invite email not configured — OTP for manual delivery",
			zap.String("to", toEmail), zap.String("otp", otp))
		return
	}
	roleLabel := RoleLabels[role]
	if roleLabel == "" {
		roleLabel = role
	}
	inviteURL := s.baseURL + "/staff/accept-invite?email=" + toEmail
	subj, html := email.StaffInvite(toEmail, name, role, roleLabel, inviteURL, otp)
	go func() {
		if err := s.emailClient.Send(toEmail, subj, html); err != nil {
			s.logger.Error("send staff invite email",
				zap.String("to", toEmail), zap.String("role", role), zap.Error(err))
		}
	}()
}
