package services

import "testing"

// The "*" wildcard (and the superadmin role that carries it) must grant control
// of every single permission in the catalogue — that is the "control every nook
// and corner" guarantee.
func TestHasPermission_GlobalWildcardGrantsEverything(t *testing.T) {
	sets := []StaffPermissionSet{
		{Role: "admin", Custom: []string{PermAll}}, // admin elevated with "*"
		{Role: "superadmin"},                       // superadmin role
		{Role: "owner"},                            // founder
	}
	for _, s := range sets {
		for _, p := range allPermissions() {
			if !HasPermission(s, p) {
				t.Errorf("role=%q custom=%v: expected permission %q to be granted", s.Role, s.Custom, p)
			}
		}
	}
}

// A "resource:*" grant covers every action on that resource and nothing else.
func TestHasPermission_ResourceWildcard(t *testing.T) {
	s := StaffPermissionSet{Role: "readonly", Custom: []string{"users:*"}}

	// All users:* actions granted.
	for _, p := range []string{PermUsersView, PermUsersManage, PermUsersDisable, PermUsersDelete, PermUsersExport} {
		if !HasPermission(s, p) {
			t.Errorf("users:* should grant %q", p)
		}
	}
	// A different resource is NOT granted (beyond readonly's own defaults).
	if HasPermission(s, PermKYCApprove) {
		t.Errorf("users:* must not grant kyc:approve")
	}
	if HasPermission(s, PermLimitsManage) {
		t.Errorf("users:* must not grant limits:manage")
	}
}

// A revoke must override even a wildcard grant — this is what makes control
// truly granular (grant a whole resource, then carve out one action).
func TestHasPermission_RevokeOverridesWildcard(t *testing.T) {
	s := StaffPermissionSet{
		Role:    "admin",
		Custom:  []string{PermAll},
		Revoked: []string{PermUsersDelete},
	}
	if HasPermission(s, PermUsersDelete) {
		t.Errorf("revoked users:delete must be denied even with '*'")
	}
	if !HasPermission(s, PermUsersDisable) {
		t.Errorf("non-revoked permissions must still be granted")
	}
}

func TestEffectivePermissions_ExpandsWildcards(t *testing.T) {
	s := StaffPermissionSet{Role: "readonly", Custom: []string{"limits:*"}, Revoked: []string{PermLimitsManage}}
	eff := EffectivePermissions(s)

	has := func(p string) bool {
		for _, e := range eff {
			if e == p {
				return true
			}
		}
		return false
	}
	if !has(PermLimitsView) {
		t.Errorf("limits:* should expand to include limits:view")
	}
	if has(PermLimitsManage) {
		t.Errorf("revoked limits:manage should be excluded from effective set")
	}
}

func TestIsValidPermission_Wildcards(t *testing.T) {
	valid := []string{PermAll, "users:*", "limits:*", PermUsersView, PermLimitsManage}
	for _, p := range valid {
		if !IsValidPermission(p) {
			t.Errorf("expected %q to be a valid permission", p)
		}
	}
	invalid := []string{"bogus:*", "users:nonsense", "notaperm", ":*"}
	for _, p := range invalid {
		if IsValidPermission(p) {
			t.Errorf("expected %q to be invalid", p)
		}
	}
}

func TestIsValidRole_SuperadminAssignableOwnerNot(t *testing.T) {
	if !IsValidRole("superadmin") {
		t.Errorf("superadmin should be assignable (owner can hand out full control)")
	}
	if IsValidRole("owner") {
		t.Errorf("owner must not be assignable via invite")
	}
}

// Every permission referenced by a role default must be a valid catalogue entry
// or a known wildcard — guards against typos in rolePermissions.
func TestRolePermissions_AllValid(t *testing.T) {
	for role, perms := range rolePermissions {
		for _, p := range perms {
			if !IsValidPermission(p) {
				t.Errorf("role %q references invalid permission %q", role, p)
			}
		}
	}
}
