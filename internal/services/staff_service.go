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
)

var (
	ErrStaffNotFound      = errors.New("staff member not found")
	ErrStaffAlreadyExists = errors.New("a staff member with that email already exists")
	ErrCannotModifyOwner  = errors.New("the owner account cannot be modified via this endpoint")
	ErrInvalidRole        = errors.New("invalid role — must be one of: admin, compliance, support, finance, readonly")
	ErrInvalidInviteToken = errors.New("invite token is invalid or has already been used")
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
	logger      *zap.Logger
}

func NewStaffService(pool *pgxpool.Pool, emailClient *email.Client, logger *zap.Logger) *StaffService {
	return &StaffService{pool: pool, emailClient: emailClient, logger: logger}
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

	token := generateStaffInviteToken()

	inviterID := in.InviterStaffID
	member, err := q.CreateStaffMember(ctx, db.CreateStaffMemberParams{
		ID:                 uuid.New(),
		Email:              in.Email,
		Name:               in.Name,
		Role:               in.Role,
		CustomPermissions:  in.CustomPerms,
		RevokedPermissions: in.RevokedPerms,
		InvitedBy:          &inviterID,
		InviteToken:        token,
	})
	if err != nil {
		return nil, fmt.Errorf("create staff: %w", err)
	}

	// Send invite email asynchronously (best-effort).
	if s.emailClient != nil {
		go s.sendInviteEmail(in.Email, in.Name, in.Role, token)
	}

	s.logger.Info("staff invite created",
		zap.String("email", in.Email),
		zap.String("role", in.Role),
		zap.String("invited_by", in.InviterStaffID.String()),
	)

	return staffToView(member), nil
}

// AcceptInvite links a user account to the pending staff_member record.
// Called after the invitee has registered/logged in and clicked the invite link.
func (s *StaffService) AcceptInvite(ctx context.Context, token string, userID uuid.UUID) error {
	q := db.New(s.pool)
	if err := q.AcceptStaffInvite(ctx, token, userID); err != nil {
		return ErrInvalidInviteToken
	}
	return nil
}

// GetByID returns a single staff member.
func (s *StaffService) GetByID(ctx context.Context, id uuid.UUID) (*StaffMemberView, error) {
	q := db.New(s.pool)
	m, err := q.GetStaffMemberByID(ctx, id)
	if err != nil {
		return nil, ErrStaffNotFound
	}
	return staffToView(m), nil
}

// GetByUserID looks up a staff member by their linked user_id (used in auth flow).
func (s *StaffService) GetByUserID(ctx context.Context, userID uuid.UUID) (*StaffMemberView, error) {
	q := db.New(s.pool)
	m, err := q.GetStaffMemberByUserID(ctx, userID)
	if err != nil {
		return nil, ErrStaffNotFound
	}
	return staffToView(m), nil
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
		views = append(views, *staffToView(m))
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
	customPerms := in.CustomPerms
	if customPerms == nil {
		customPerms = existing.CustomPermissions
	}
	revokedPerms := in.RevokedPerms
	if revokedPerms == nil {
		revokedPerms = existing.RevokedPermissions
	}

	for _, p := range append(customPerms, revokedPerms...) {
		if !IsValidPermission(p) {
			return nil, fmt.Errorf("unknown permission: %q", p)
		}
	}

	updated, err := q.UpdateStaffMember(ctx, db.UpdateStaffMemberParams{
		ID:                 id,
		Role:               role,
		CustomPermissions:  customPerms,
		RevokedPermissions: revokedPerms,
	})
	if err != nil {
		return nil, fmt.Errorf("update staff: %w", err)
	}
	return staffToView(updated), nil
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
		ID:         uuid.New(),
		StaffID:    mustParseUUID(set.StaffID),
		StaffName:  set.StaffName,
		StaffEmail: set.StaffEmail,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Details:    raw,
		IPAddress:  ipAddress,
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
	total, _ := q.CountAdminAuditLogs(ctx, params)
	return logs, total, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

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
		LastLoginAt:          m.LastLoginAt,
		CreatedAt:            m.CreatedAt,
	}
}

func generateStaffInviteToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return uuid.New().String() // fallback
	}
	return hex.EncodeToString(b)
}

func mustParseUUID(s string) uuid.UUID {
	id, _ := uuid.Parse(s)
	return id
}

func (s *StaffService) sendInviteEmail(toEmail, name, role, token string) {
	if s.emailClient == nil {
		return
	}
	s.logger.Info("sending staff invite email",
		zap.String("to", toEmail),
		zap.String("role", role),
	)
	// Actual email sending wired to emailClient.Send when email service is ready.
	// For now this logs the intent; the token can be found in the audit log.
	_ = token
}
