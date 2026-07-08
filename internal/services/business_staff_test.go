package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestInviteMerchantStaff_InvalidRole(t *testing.T) {
	s := &BusinessService{}
	_, _, err := s.InviteMerchantStaff(context.Background(), uuid.New(), "test@example.com", "invalid_role")
	if err != ErrInvalidMerchantRole {
		t.Errorf("expected ErrInvalidMerchantRole, got %v", err)
	}
}

func TestUpdateMerchantStaffRole_InvalidRole(t *testing.T) {
	s := &BusinessService{}
	err := s.UpdateMerchantStaffRole(context.Background(), uuid.New(), uuid.New(), "invalid_role")
	if err != ErrInvalidMerchantRole {
		t.Errorf("expected ErrInvalidMerchantRole, got %v", err)
	}
}
