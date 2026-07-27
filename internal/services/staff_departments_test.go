package services

import "testing"

func TestAdminRoleHasStaffAndDepartmentPerms(t *testing.T) {
	admin := StaffPermissionSet{Role: "admin"}
	for _, p := range []string{PermStaffInvite, PermStaffRemove, PermDepartmentsView, PermDepartmentsManage} {
		if !HasPermission(admin, p) {
			t.Errorf("admin role should have %q via wildcard grants", p)
		}
	}

	// readonly must NOT be able to manage departments or remove staff.
	ro := StaffPermissionSet{Role: "readonly"}
	for _, p := range []string{PermDepartmentsManage, PermStaffRemove} {
		if HasPermission(ro, p) {
			t.Errorf("readonly role must not have %q", p)
		}
	}
}

func TestNewPermissionsAreInCatalogue(t *testing.T) {
	for _, p := range []string{PermStaffRemove, PermDepartmentsView, PermDepartmentsManage} {
		if !IsValidPermission(p) {
			t.Errorf("permission %q must be registered in the catalogue", p)
		}
	}
}

func TestGenerateStaffInviteOTP(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		otp := generateStaffInviteOTP()
		if len(otp) != 6 {
			t.Fatalf("OTP %q length = %d, want 6", otp, len(otp))
		}
		for _, c := range otp {
			if c < '0' || c > '9' {
				t.Fatalf("OTP %q contains non-digit %q", otp, string(c))
			}
		}
		seen[otp] = true
	}
	if len(seen) < 40 {
		t.Errorf("OTP not random enough: %d distinct of 50", len(seen))
	}
}
