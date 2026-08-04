package services

import (
	"testing"

	"github.com/google/uuid"
)

func TestMomoSupportedCurrency(t *testing.T) {
	for _, c := range []string{"XOF", "XAF"} {
		if !momoSupportedCurrency(c) {
			t.Errorf("expected %s supported", c)
		}
	}
	for _, c := range []string{"USD", "EUR", "NGN", "", "xof"} {
		if momoSupportedCurrency(c) {
			t.Errorf("expected %q unsupported", c)
		}
	}
}

func TestStaffPtr(t *testing.T) {
	if staffPtr(uuid.Nil) != nil {
		t.Error("staffPtr(Nil) must be nil so reviewed_by stores NULL for the owner")
	}
	id := uuid.New()
	if p := staffPtr(id); p == nil || *p != id {
		t.Error("staffPtr(id) must return &id for a real staff member")
	}
}

func TestPageBounds(t *testing.T) {
	cases := []struct {
		page, perPage        int32
		wantLimit, wantOffset int32
	}{
		{0, 0, 20, 0},     // defaults
		{1, 20, 20, 0},    // page 1
		{2, 20, 20, 20},   // page 2 offset
		{3, 50, 50, 100},  // custom per-page
		{1, 500, 20, 0},   // over-max per-page clamps to default
		{-5, -5, 20, 0},   // negatives normalise
	}
	for _, c := range cases {
		l, o := pageBounds(c.page, c.perPage)
		if l != c.wantLimit || o != c.wantOffset {
			t.Errorf("pageBounds(%d,%d) = (%d,%d), want (%d,%d)", c.page, c.perPage, l, o, c.wantLimit, c.wantOffset)
		}
	}
}

func TestStrPtrOrNil(t *testing.T) {
	if strPtrOrNil("   ") != nil {
		t.Error("blank string must map to nil (NULL)")
	}
	if p := strPtrOrNil("wave"); p == nil || *p != "wave" {
		t.Error("non-empty string must be preserved")
	}
}
