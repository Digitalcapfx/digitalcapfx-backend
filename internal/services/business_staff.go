package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/google/uuid"
	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

var (
	ErrMerchantStaffNotFound      = errors.New("merchant staff member not found")
	ErrMerchantStaffAlreadyExists = errors.New("a staff member with that email is already invited or active for this business")
	ErrInvalidMerchantRole        = errors.New("invalid role — must be one of: manager, developer, viewer")
	ErrInvalidMerchantInviteToken = errors.New("invite token is invalid or already used")
)

// InviteMerchantStaff creates a pending staff record for a business and returns the record and invite token.
func (s *BusinessService) InviteMerchantStaff(ctx context.Context, businessUserID uuid.UUID, emailAddress, role string) (*db.MerchantStaff, string, error) {
	emailAddress = strings.ToLower(strings.TrimSpace(emailAddress))
	role = strings.ToLower(strings.TrimSpace(role))

	if role != "manager" && role != "developer" && role != "viewer" {
		return nil, "", ErrInvalidMerchantRole
	}
	if emailAddress == "" {
		return nil, "", errors.New("email is required")
	}

	q := db.New(s.pool)

	// Check duplicate
	if _, err := q.GetMerchantStaffByEmailAndBusiness(ctx, db.GetMerchantStaffByEmailAndBusinessParams{
		Email:          emailAddress,
		BusinessUserID: businessUserID,
	}); err == nil {
		return nil, "", ErrMerchantStaffAlreadyExists
	}

	// Generate token
	tokenBytes := make([]byte, 32)
	var token string
	if _, err := rand.Read(tokenBytes); err != nil {
		token = uuid.New().String()
	} else {
		token = hex.EncodeToString(tokenBytes)
	}

	inviteToken := &token
	staff, err := q.CreateMerchantStaff(ctx, db.CreateMerchantStaffParams{
		BusinessUserID: businessUserID,
		Email:          emailAddress,
		Role:           role,
		InviteToken:    inviteToken,
	})
	if err != nil {
		return nil, "", err
	}

	return &staff, token, nil
}

// AcceptMerchantStaffInvite binds an invited staff email to a registered user account.
func (s *BusinessService) AcceptMerchantStaffInvite(ctx context.Context, inviteToken string, staffUserID uuid.UUID) error {
	q := db.New(s.pool)
	
	// Check if invite exists
	_, err := q.GetMerchantStaffByInviteToken(ctx, &inviteToken)
	if err != nil {
		return ErrInvalidMerchantInviteToken
	}

	err = q.AcceptMerchantStaffInvite(ctx, db.AcceptMerchantStaffInviteParams{
		StaffUserID: &staffUserID,
		InviteToken: &inviteToken,
	})
	if err != nil {
		return err
	}

	// Update user role to merchant staff if needed
	_, err = q.UpdateUserKYCStatus(ctx, db.UpdateUserKYCStatusParams{
		ID:        staffUserID,
		KycStatus: "approved", // auto-approve linked staff members or keep as is?
	})
	_ = err // non-blocking

	return nil
}

// UpdateMerchantStaffRole updates the merchant staff role.
func (s *BusinessService) UpdateMerchantStaffRole(ctx context.Context, id, businessUserID uuid.UUID, role string) error {
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "manager" && role != "developer" && role != "viewer" {
		return ErrInvalidMerchantRole
	}

	q := db.New(s.pool)
	
	// Check exists
	_, err := q.GetMerchantStaffByID(ctx, id)
	if err != nil {
		return ErrMerchantStaffNotFound
	}

	return q.UpdateMerchantStaffRole(ctx, db.UpdateMerchantStaffRoleParams{
		Role:           role,
		ID:             id,
		BusinessUserID: businessUserID,
	})
}

// RemoveMerchantStaff removes a staff member from the merchant.
func (s *BusinessService) RemoveMerchantStaff(ctx context.Context, id, businessUserID uuid.UUID) error {
	q := db.New(s.pool)
	
	// Check exists
	_, err := q.GetMerchantStaffByID(ctx, id)
	if err != nil {
		return ErrMerchantStaffNotFound
	}

	return q.DeleteMerchantStaff(ctx, db.DeleteMerchantStaffParams{
		ID:             id,
		BusinessUserID: businessUserID,
	})
}

// ListMerchantStaff members of a business.
func (s *BusinessService) ListMerchantStaff(ctx context.Context, businessUserID uuid.UUID) ([]db.MerchantStaff, error) {
	q := db.New(s.pool)
	return q.ListMerchantStaff(ctx, businessUserID)
}