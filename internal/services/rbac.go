package services

// ─── Permission constants ─────────────────────────────────────────────────────
// Format: "resource:action"

const (
	// User management
	PermUsersView     = "users:view"
	PermUsersDisable  = "users:disable"
	PermUsersEnable   = "users:enable"
	PermUsersResetKYC = "users:reset_kyc"

	// KYC
	PermKYCView    = "kyc:view"
	PermKYCApprove = "kyc:approve"
	PermKYCReject  = "kyc:reject"

	// Transactions
	PermTxView = "transactions:view"

	// Withdrawals
	PermWithdrawalsView    = "withdrawals:view"
	PermWithdrawalsApprove = "withdrawals:approve"
	PermWithdrawalsReject  = "withdrawals:reject"
	PermWithdrawalsRates   = "withdrawals:rates"

	// Staff management
	PermStaffView    = "staff:view"
	PermStaffInvite  = "staff:invite"
	PermStaffUpdate  = "staff:update"
	PermStaffDisable = "staff:disable"

	// Support
	PermSupportView    = "support:view"
	PermSupportRespond = "support:respond"

	// Analytics / audit
	PermAnalyticsView = "analytics:view"
	PermAuditView     = "audit:view"

	// Wildcard — owner only
	PermAll = "*"
)

// ─── Role definitions ─────────────────────────────────────────────────────────

// ValidRoles lists every assignable role. "owner" is set only on the founder account.
var ValidRoles = []string{"owner", "admin", "compliance", "support", "finance", "readonly"}

// RoleLabels maps role keys to display names.
var RoleLabels = map[string]string{
	"owner":      "Platform Owner",
	"admin":      "Administrator",
	"compliance": "Compliance Officer",
	"support":    "Customer Support",
	"finance":    "Finance Manager",
	"readonly":   "Read-Only Viewer",
}

// RoleDescriptions describes each role for the invite UI.
var RoleDescriptions = map[string]string{
	"owner":      "Full control over everything, including staff management.",
	"admin":      "Can manage users, KYC, withdrawals, and support. Cannot manage other admins.",
	"compliance": "Can review and decide on KYC submissions. View-only for other areas.",
	"support":    "Can view users and manage support tickets.",
	"finance":    "Can approve/reject withdrawals and manage FX rates.",
	"readonly":   "View-only access to users, transactions, and analytics.",
}

// rolePermissions maps each role to the set of permissions it carries by default.
// The "owner" role uses the wildcard "*" which is checked separately in HasPermission.
var rolePermissions = map[string][]string{
	"owner": {PermAll},
	"admin": {
		PermUsersView, PermUsersDisable, PermUsersEnable, PermUsersResetKYC,
		PermKYCView, PermKYCApprove, PermKYCReject,
		PermTxView,
		PermWithdrawalsView, PermWithdrawalsApprove, PermWithdrawalsReject, PermWithdrawalsRates,
		PermStaffView,
		PermSupportView, PermSupportRespond,
		PermAnalyticsView, PermAuditView,
	},
	"compliance": {
		PermUsersView,
		PermKYCView, PermKYCApprove, PermKYCReject,
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
		PermTxView,
		PermWithdrawalsView, PermWithdrawalsApprove, PermWithdrawalsReject, PermWithdrawalsRates,
		PermAnalyticsView,
	},
	"readonly": {
		PermUsersView,
		PermTxView,
		PermAnalyticsView,
	},
}

// ─── Permission resolution ────────────────────────────────────────────────────

// StaffPermissionSet is loaded by the permission middleware and stored in context.
// It carries everything needed to evaluate any single HasPermission call.
type StaffPermissionSet struct {
	StaffID   string
	StaffName  string
	StaffEmail string
	Role      string
	Custom    []string // additional grants beyond role defaults
	Revoked   []string // revocations of role defaults
}

// HasPermission returns true if the set allows the requested permission.
// Evaluation order:
//  1. Owner role → always true.
//  2. Revoked list → always false if matched.
//  3. Role default permissions → true if matched (or wildcard).
//  4. Custom permissions → true if matched (or wildcard).
func HasPermission(s StaffPermissionSet, required string) bool {
	if s.Role == "owner" {
		return true
	}
	for _, r := range s.Revoked {
		if r == required {
			return false
		}
	}
	for _, p := range rolePermissions[s.Role] {
		if p == PermAll || p == required {
			return true
		}
	}
	for _, p := range s.Custom {
		if p == PermAll || p == required {
			return true
		}
	}
	return false
}

// EffectivePermissions returns the full resolved permission list for display.
func EffectivePermissions(s StaffPermissionSet) []string {
	if s.Role == "owner" {
		all := allPermissions()
		return all
	}
	seen := map[string]bool{}
	revoked := map[string]bool{}
	for _, r := range s.Revoked {
		revoked[r] = true
	}
	var result []string
	for _, p := range rolePermissions[s.Role] {
		if !revoked[p] && !seen[p] {
			result = append(result, p)
			seen[p] = true
		}
	}
	for _, p := range s.Custom {
		if !revoked[p] && !seen[p] {
			result = append(result, p)
			seen[p] = true
		}
	}
	return result
}

// RolePermissions returns the default permissions for a named role.
func RolePermissions(role string) []string {
	return rolePermissions[role]
}

func allPermissions() []string {
	return []string{
		PermUsersView, PermUsersDisable, PermUsersEnable, PermUsersResetKYC,
		PermKYCView, PermKYCApprove, PermKYCReject,
		PermTxView,
		PermWithdrawalsView, PermWithdrawalsApprove, PermWithdrawalsReject, PermWithdrawalsRates,
		PermStaffView, PermStaffInvite, PermStaffUpdate, PermStaffDisable,
		PermSupportView, PermSupportRespond,
		PermAnalyticsView, PermAuditView,
	}
}

// IsValidRole returns true if the role string is an assignable staff role.
// "owner" is intentionally excluded — you cannot invite someone as owner.
func IsValidRole(role string) bool {
	for _, r := range ValidRoles {
		if r == role {
			return r != "owner" // owner cannot be assigned via invite
		}
	}
	return false
}

// IsValidPermission returns true if perm is a known permission constant.
func IsValidPermission(perm string) bool {
	for _, p := range allPermissions() {
		if p == perm {
			return true
		}
	}
	return false
}
