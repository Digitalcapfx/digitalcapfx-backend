package services

import "strings"

// ─── Permission constants ─────────────────────────────────────────────────────
// Format: "resource:action". Two wildcard forms are also grantable:
//   "*"          → every permission (full platform control)
//   "resource:*" → every action on one resource (e.g. "users:*")

const (
	// User management
	PermUsersView     = "users:view"
	PermUsersDisable  = "users:disable"
	PermUsersEnable   = "users:enable"
	PermUsersResetKYC = "users:reset_kyc"
	PermUsersManage   = "users:manage" // edit profile, change account type
	PermUsersExport   = "users:export" // export user data
	PermUsersDelete   = "users:delete" // hard/soft delete a user

	// KYC
	PermKYCView    = "kyc:view"
	PermKYCApprove = "kyc:approve"
	PermKYCReject  = "kyc:reject"

	// Transactions
	PermTxView   = "transactions:view"
	PermTxExport = "transactions:export"
	PermTxAdjust = "transactions:adjust" // manual credit/debit/reversal

	// Withdrawals
	PermWithdrawalsView    = "withdrawals:view"
	PermWithdrawalsApprove = "withdrawals:approve"
	PermWithdrawalsReject  = "withdrawals:reject"
	PermWithdrawalsRates   = "withdrawals:rates"

	// Limits & tiers
	PermLimitsView   = "limits:view"
	PermLimitsManage = "limits:manage" // edit tier limits + per-user overrides

	// Fees
	PermFeesView   = "fees:view"
	PermFeesManage = "fees:manage"

	// Staff management
	PermStaffView    = "staff:view"
	PermStaffInvite  = "staff:invite"
	PermStaffUpdate  = "staff:update"
	PermStaffDisable = "staff:disable"

	// Roles / permission catalogue
	PermRolesView = "roles:view"

	// Support
	PermSupportView    = "support:view"
	PermSupportRespond = "support:respond"

	// Notifications (platform-wide broadcast)
	PermNotificationsBroadcast = "notifications:broadcast"

	// Platform config / feature flags
	PermConfigView   = "config:view"
	PermConfigManage = "config:manage"

	// Analytics / audit
	PermAnalyticsView = "analytics:view"
	PermAuditView     = "audit:view"

	// Wildcard — full platform control.
	PermAll = "*"
)

// ─── Permission catalogue ──────────────────────────────────────────────────────
// The catalogue is the single source of truth: every permission the platform
// understands is listed here, grouped by resource. allPermissions(),
// IsValidPermission and the /admin/permissions endpoint all derive from it, so
// there is exactly one place to add a new controllable surface.

type PermissionMeta struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
}

type PermissionGroup struct {
	Resource    string           `json:"resource"`
	Label       string           `json:"label"`
	Description string           `json:"description"`
	Wildcard    string           `json:"wildcard"` // grant to control the whole group, e.g. "users:*"
	Permissions []PermissionMeta `json:"permissions"`
}

var permissionCatalogue = []PermissionGroup{
	{
		Resource: "users", Label: "Users", Description: "Customer accounts and their data.", Wildcard: "users:*",
		Permissions: []PermissionMeta{
			{PermUsersView, "View users", "List and view customer accounts and details.", "users", "view"},
			{PermUsersManage, "Manage users", "Edit profiles and change account type (individual/business).", "users", "manage"},
			{PermUsersDisable, "Disable users", "Suspend a customer account.", "users", "disable"},
			{PermUsersEnable, "Enable users", "Re-activate a suspended account.", "users", "enable"},
			{PermUsersResetKYC, "Reset KYC", "Reset a user's KYC status to allow re-verification.", "users", "reset_kyc"},
			{PermUsersExport, "Export users", "Export customer data.", "users", "export"},
			{PermUsersDelete, "Delete users", "Delete a customer account.", "users", "delete"},
		},
	},
	{
		Resource: "kyc", Label: "KYC / Compliance", Description: "Identity verification review.", Wildcard: "kyc:*",
		Permissions: []PermissionMeta{
			{PermKYCView, "View KYC", "View pending and completed KYC submissions.", "kyc", "view"},
			{PermKYCApprove, "Approve KYC", "Approve a KYC submission.", "kyc", "approve"},
			{PermKYCReject, "Reject KYC", "Reject a KYC submission.", "kyc", "reject"},
		},
	},
	{
		Resource: "transactions", Label: "Transactions", Description: "Customer transaction history.", Wildcard: "transactions:*",
		Permissions: []PermissionMeta{
			{PermTxView, "View transactions", "View any user's transactions.", "transactions", "view"},
			{PermTxExport, "Export transactions", "Export transaction data.", "transactions", "export"},
			{PermTxAdjust, "Adjust transactions", "Manual credit, debit or reversal.", "transactions", "adjust"},
		},
	},
	{
		Resource: "withdrawals", Label: "Withdrawals", Description: "Withdrawal review and FX rates.", Wildcard: "withdrawals:*",
		Permissions: []PermissionMeta{
			{PermWithdrawalsView, "View withdrawals", "View withdrawal requests.", "withdrawals", "view"},
			{PermWithdrawalsApprove, "Approve withdrawals", "Approve a withdrawal.", "withdrawals", "approve"},
			{PermWithdrawalsReject, "Reject withdrawals", "Reject a withdrawal.", "withdrawals", "reject"},
			{PermWithdrawalsRates, "Manage FX rates", "Set withdrawal FX rates.", "withdrawals", "rates"},
		},
	},
	{
		Resource: "limits", Label: "Limits & Tiers", Description: "Account-tier limits and per-user overrides.", Wildcard: "limits:*",
		Permissions: []PermissionMeta{
			{PermLimitsView, "View limits", "View tier limits and per-user overrides.", "limits", "view"},
			{PermLimitsManage, "Manage limits", "Edit tier limits and set per-user overrides.", "limits", "manage"},
		},
	},
	{
		Resource: "fees", Label: "Fees", Description: "Platform fee configuration.", Wildcard: "fees:*",
		Permissions: []PermissionMeta{
			{PermFeesView, "View fees", "View fee configuration.", "fees", "view"},
			{PermFeesManage, "Manage fees", "Edit fee configuration.", "fees", "manage"},
		},
	},
	{
		Resource: "staff", Label: "Staff", Description: "Admin/staff accounts.", Wildcard: "staff:*",
		Permissions: []PermissionMeta{
			{PermStaffView, "View staff", "List and view staff members.", "staff", "view"},
			{PermStaffInvite, "Invite staff", "Invite a new staff member.", "staff", "invite"},
			{PermStaffUpdate, "Update staff", "Change a staff member's role and permissions.", "staff", "update"},
			{PermStaffDisable, "Disable staff", "Disable or re-enable a staff member.", "staff", "disable"},
		},
	},
	{
		Resource: "roles", Label: "Roles", Description: "Role and permission catalogue.", Wildcard: "roles:*",
		Permissions: []PermissionMeta{
			{PermRolesView, "View roles", "View roles and the permission catalogue.", "roles", "view"},
		},
	},
	{
		Resource: "support", Label: "Support", Description: "Customer support tickets.", Wildcard: "support:*",
		Permissions: []PermissionMeta{
			{PermSupportView, "View support", "View support tickets.", "support", "view"},
			{PermSupportRespond, "Respond to support", "Respond to support tickets.", "support", "respond"},
		},
	},
	{
		Resource: "notifications", Label: "Notifications", Description: "Platform-wide notifications.", Wildcard: "notifications:*",
		Permissions: []PermissionMeta{
			{PermNotificationsBroadcast, "Broadcast notifications", "Send platform-wide notifications.", "notifications", "broadcast"},
		},
	},
	{
		Resource: "config", Label: "Platform Config", Description: "Platform settings and feature flags.", Wildcard: "config:*",
		Permissions: []PermissionMeta{
			{PermConfigView, "View config", "View platform settings and feature flags.", "config", "view"},
			{PermConfigManage, "Manage config", "Edit platform settings and feature flags.", "config", "manage"},
		},
	},
	{
		Resource: "analytics", Label: "Analytics", Description: "Dashboards and reporting.", Wildcard: "analytics:*",
		Permissions: []PermissionMeta{
			{PermAnalyticsView, "View analytics", "View admin dashboards and analytics.", "analytics", "view"},
		},
	},
	{
		Resource: "audit", Label: "Audit Log", Description: "Admin action audit trail.", Wildcard: "audit:*",
		Permissions: []PermissionMeta{
			{PermAuditView, "View audit log", "View the admin audit log.", "audit", "view"},
		},
	},
}

// PermissionCatalogue returns the full grouped catalogue for the admin UI.
func PermissionCatalogue() []PermissionGroup {
	return permissionCatalogue
}

// ─── Role definitions ─────────────────────────────────────────────────────────

// ValidRoles lists every assignable role. "owner" is set only on the founder
// account and cannot be assigned via invite. "superadmin" carries the same full
// control (via the "*" wildcard) but CAN be granted — this is how an owner hands
// full platform control to another admin.
var ValidRoles = []string{"owner", "superadmin", "admin", "compliance", "support", "finance", "readonly"}

var RoleLabels = map[string]string{
	"owner":      "Platform Owner",
	"superadmin": "Super Administrator",
	"admin":      "Administrator",
	"compliance": "Compliance Officer",
	"support":    "Customer Support",
	"finance":    "Finance Manager",
	"readonly":   "Read-Only Viewer",
}

var RoleDescriptions = map[string]string{
	"owner":      "Full control over everything, including staff management. Founder account.",
	"superadmin": "Full control over every function, feature and setting on the platform.",
	"admin":      "Manage users, KYC, withdrawals, limits and support. Cannot manage other admins by default.",
	"compliance": "Review and decide on KYC submissions. View-only elsewhere.",
	"support":    "View users and manage support tickets.",
	"finance":    "Approve/reject withdrawals, manage FX rates and view limits.",
	"readonly":   "View-only access to users, transactions and analytics.",
}

// rolePermissions maps each role to its default permissions. "owner" and
// "superadmin" use the "*" wildcard, resolved in HasPermission.
var rolePermissions = map[string][]string{
	"owner":      {PermAll},
	"superadmin": {PermAll},
	"admin": {
		"users:*",
		"kyc:*",
		PermTxView, PermTxExport,
		"withdrawals:*",
		"limits:*",
		"fees:*",
		PermStaffView,
		PermRolesView,
		"support:*",
		PermNotificationsBroadcast,
		PermAnalyticsView, PermAuditView,
	},
	"compliance": {
		PermUsersView,
		"kyc:*",
		PermTxView,
		PermSupportView,
	},
	"support": {
		PermUsersView,
		PermSupportView, PermSupportRespond,
		PermTxView,
	},
	"finance": {
		PermUsersView,
		PermTxView, PermTxExport,
		"withdrawals:*",
		PermLimitsView,
		PermFeesView,
		PermAnalyticsView,
	},
	"readonly": {
		PermUsersView,
		PermTxView,
		PermLimitsView,
		PermAnalyticsView,
	},
}

// ─── Permission resolution ────────────────────────────────────────────────────

type StaffPermissionSet struct {
	StaffID    string
	StaffName  string
	StaffEmail string
	Role       string
	Custom     []string // additional grants beyond role defaults
	Revoked    []string // revocations of specific permissions (override grants)
}

// HasPermission returns true if the set allows the requested permission.
// Evaluation order:
//  1. Owner role → always true.
//  2. Revoked list → always false if it names the exact permission (a revoke
//     overrides even a "*" or "resource:*" grant — that is what makes control
//     granular).
//  3. Role defaults + custom grants → true if any grant matches (exact, global
//     wildcard, or resource wildcard).
func HasPermission(s StaffPermissionSet, required string) bool {
	if s.Role == "owner" {
		return true
	}
	for _, r := range s.Revoked {
		if r == required {
			return false
		}
	}
	for _, g := range rolePermissions[s.Role] {
		if permMatches(g, required) {
			return true
		}
	}
	for _, g := range s.Custom {
		if permMatches(g, required) {
			return true
		}
	}
	return false
}

// permMatches reports whether a granted permission covers the required one,
// honouring the "*" (global) and "resource:*" (whole-resource) wildcards.
func permMatches(grant, required string) bool {
	if grant == PermAll || grant == required {
		return true
	}
	if strings.HasSuffix(grant, ":*") {
		res := strings.TrimSuffix(grant, ":*")
		return strings.HasPrefix(required, res+":")
	}
	return false
}

// EffectivePermissions returns the fully-expanded, revoke-filtered permission
// list for display (wildcards expanded to their concrete permissions).
func EffectivePermissions(s StaffPermissionSet) []string {
	if s.Role == "owner" {
		return allPermissions()
	}
	revoked := map[string]bool{}
	for _, r := range s.Revoked {
		revoked[r] = true
	}
	grants := append(append([]string{}, rolePermissions[s.Role]...), s.Custom...)

	all := allPermissions()
	granted := map[string]bool{}
	for _, g := range grants {
		if g == PermAll {
			for _, p := range all {
				granted[p] = true
			}
			break
		}
	}
	for _, g := range grants {
		if strings.HasSuffix(g, ":*") {
			res := strings.TrimSuffix(g, ":*")
			for _, p := range all {
				if strings.HasPrefix(p, res+":") {
					granted[p] = true
				}
			}
		} else if g != PermAll {
			granted[g] = true
		}
	}

	result := make([]string, 0, len(granted))
	for _, p := range all { // stable catalogue order
		if granted[p] && !revoked[p] {
			result = append(result, p)
		}
	}
	return result
}

// RolePermissions returns the default permissions for a named role.
func RolePermissions(role string) []string {
	return rolePermissions[role]
}

// allPermissions flattens the catalogue into the full list of concrete keys.
func allPermissions() []string {
	out := make([]string, 0, 32)
	for _, g := range permissionCatalogue {
		for _, p := range g.Permissions {
			out = append(out, p.Key)
		}
	}
	return out
}

func knownResources() map[string]bool {
	m := make(map[string]bool, len(permissionCatalogue))
	for _, g := range permissionCatalogue {
		m[g.Resource] = true
	}
	return m
}

// IsValidRole returns true if the role string is assignable via invite.
// "owner" is intentionally excluded — it is the founder account and cannot be
// handed out — but "superadmin" (equivalent power) can be assigned.
func IsValidRole(role string) bool {
	for _, r := range ValidRoles {
		if r == role {
			return r != "owner"
		}
	}
	return false
}

// IsValidPermission returns true if perm is a known concrete permission, the
// global wildcard "*", or a "resource:*" wildcard for a known resource.
func IsValidPermission(perm string) bool {
	if perm == PermAll {
		return true
	}
	if strings.HasSuffix(perm, ":*") {
		return knownResources()[strings.TrimSuffix(perm, ":*")]
	}
	for _, p := range allPermissions() {
		if p == perm {
			return true
		}
	}
	return false
}
