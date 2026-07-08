package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

var (
	ErrBusinessProfileNotFound = errors.New("business profile not found")
	ErrDirectorNotFound        = errors.New("director not found")
	ErrInvalidDateFormat       = errors.New("date_of_birth must be in YYYY-MM-DD format")
)

type BusinessKYCStatus struct {
	ProfileComplete   bool `json:"profile_complete"`
	DirectorsComplete bool `json:"directors_complete"`
	DocumentsComplete bool `json:"documents_complete"`
	OverallComplete   bool `json:"overall_complete"`
}

type BusinessService struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

func NewBusinessService(pool *pgxpool.Pool, logger *zap.Logger) *BusinessService {
	return &BusinessService{pool: pool, logger: logger}
}

func (s *BusinessService) GetProfile(ctx context.Context, userID uuid.UUID) (*db.BusinessProfile, error) {
	q := db.New(s.pool)
	profile, err := q.GetBusinessProfileByUserID(ctx, userID)
	if err != nil {
		return nil, ErrBusinessProfileNotFound
	}
	return &profile, nil
}

func (s *BusinessService) GetKYCStatus(ctx context.Context, userID uuid.UUID) (*BusinessKYCStatus, error) {
	q := db.New(s.pool)
	profile, err := q.GetBusinessProfileByUserID(ctx, userID)
	if err != nil {
		return &BusinessKYCStatus{}, nil
	}

	overall := profile.DirectorsComplete && profile.DocumentsComplete
	return &BusinessKYCStatus{
		ProfileComplete:   true,
		DirectorsComplete: profile.DirectorsComplete,
		DocumentsComplete: profile.DocumentsComplete,
		OverallComplete:   overall,
	}, nil
}

func (s *BusinessService) SaveProfile(
	ctx context.Context,
	userID uuid.UUID,
	companyLegalName, companyRegistrationNo, industry, countryOfIncorporation, annualRevenue, businessWebsite string,
) (*db.BusinessProfile, error) {
	q := db.New(s.pool)
	
	// Try creating
	var website *string
	if businessWebsite != "" {
		website = &businessWebsite
	}
	
	profile, err := q.SaveBusinessProfile(ctx, db.SaveBusinessProfileParams{
		UserID:                 userID,
		CompanyLegalName:       companyLegalName,
		CompanyRegistrationNo:  companyRegistrationNo,
		Industry:               industry,
		CountryOfIncorporation: countryOfIncorporation,
		AnnualRevenue:          annualRevenue,
		BusinessWebsite:        website,
	})
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (s *BusinessService) ListDirectors(ctx context.Context, userID uuid.UUID) ([]db.BusinessDirector, error) {
	q := db.New(s.pool)
	return q.ListBusinessDirectors(ctx, userID)
}

func (s *BusinessService) AddDirector(
	ctx context.Context,
	userID uuid.UUID,
	firstName, lastName, jobTitle, dobStr, nationality, phoneNumber string,
) (*db.BusinessDirector, error) {
	dob, err := time.Parse("2006-01-02", dobStr)
	if err != nil {
		return nil, ErrInvalidDateFormat
	}

	q := db.New(s.pool)
	director, err := q.CreateBusinessDirector(ctx, db.CreateBusinessDirectorParams{
		UserID:       userID,
		FirstName:    firstName,
		LastName:     lastName,
		JobTitle:     jobTitle,
		DateOfBirth:  dob,
		Nationality:  nationality,
		PhoneNumber:  phoneNumber,
	})
	if err != nil {
		return nil, err
	}
	return &director, nil
}

func (s *BusinessService) DeleteDirector(ctx context.Context, id, userID uuid.UUID) error {
	q := db.New(s.pool)
	return q.DeleteBusinessDirector(ctx, db.DeleteBusinessDirectorParams{
		ID:     id,
		UserID: userID,
	})
}

func (s *BusinessService) InviteStaff(ctx context.Context, businessUserID uuid.UUID, email, role string) (*db.MerchantStaff, error) {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	token := hex.EncodeToString(b)

	q := db.New(s.pool)
	staff, err := q.CreateMerchantStaff(ctx, db.CreateMerchantStaffParams{
		BusinessUserID: businessUserID,
		Email:          email,
		Role:           role,
		InviteToken:    &token,
	})
	if err != nil {
		return nil, err
	}
	return &staff, nil
}

func (s *BusinessService) AcceptInvite(ctx context.Context, inviteToken string, staffUserID uuid.UUID) error {
	q := db.New(s.pool)
	return q.AcceptMerchantStaffInvite(ctx, db.AcceptMerchantStaffInviteParams{
		StaffUserID: &staffUserID,
		InviteToken: &inviteToken,
	})
}

func (s *BusinessService) ListStaff(ctx context.Context, businessUserID uuid.UUID) ([]db.MerchantStaff, error) {
	q := db.New(s.pool)
	return q.ListMerchantStaff(ctx, businessUserID)
}

func (s *BusinessService) DeleteStaff(ctx context.Context, id, businessUserID uuid.UUID) error {
	q := db.New(s.pool)
	return q.DeleteMerchantStaff(ctx, db.DeleteMerchantStaffParams{
		ID:             id,
		BusinessUserID: businessUserID,
	})
}

func (s *BusinessService) UpdateStaffRole(ctx context.Context, id, businessUserID uuid.UUID, role string) error {
	q := db.New(s.pool)
	return q.UpdateMerchantStaffRole(ctx, db.UpdateMerchantStaffRoleParams{
		Role:           role,
		ID:             id,
		BusinessUserID: businessUserID,
	})
}