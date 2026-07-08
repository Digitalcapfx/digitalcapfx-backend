// Hand-written composite types that sqlc does not generate.
// These extend or flatten generated models for service-layer convenience.
package db

import (
	"time"

	"github.com/google/uuid"
)

// UserFull is the User extended with profile fields from migration 000003.
// Services should use UserFull when they need profile fields.
type UserFull struct {
	ID              uuid.UUID `json:"id"`
	PhoneNumber     string    `json:"phone_number"`
	Email           *string   `json:"email"`
	FirstName       string    `json:"first_name"`
	LastName        string    `json:"last_name"`
	PinHash         *string   `json:"pin_hash"` // nullable for social-auth users
	KycStatus       string    `json:"kyc_status"`
	IsActive        bool      `json:"is_active"`
	Role            string    `json:"role"`
	AuthProvider    string    `json:"auth_provider"`
	GoogleSub       *string   `json:"google_sub"`
	Bio             *string   `json:"bio"`
	AvatarURL       *string   `json:"avatar_url"`
	DateOfBirth     *string   `json:"date_of_birth"` // ISO date string YYYY-MM-DD
	Nationality     *string   `json:"nationality"`
	IsEmailVerified bool      `json:"is_email_verified"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// FXRate is a hand-written view of the fx_rates table with the rate as a
// decimal string (the generated FxRate uses pgtype.Numeric).
type FXRate struct {
	BaseCurrency  string    `json:"base_currency"`
	QuoteCurrency string    `json:"quote_currency"`
	Rate          string    `json:"rate"` // decimal string
	Source        string    `json:"source"`
	FetchedAt     time.Time `json:"fetched_at"`
}

// AccountWithNilos extends Account with Nilos-specific fields from migration 000005.
type AccountWithNilos struct {
	ID               uuid.UUID `json:"id"`
	UserID           uuid.UUID `json:"user_id"`
	Currency         string    `json:"currency"`
	Balance          string    `json:"balance"`           // from pgtype.Numeric as string
	AvailableBalance string    `json:"available_balance"` // from pgtype.Numeric as string
	AccountNumber    string    `json:"account_number"`
	Status           string    `json:"status"`
	NilosAccountID   *string   `json:"nilos_account_id"`
	NilosCustomerID  *string   `json:"nilos_customer_id"`
	IBAN             *string   `json:"iban"`
	BIC              *string   `json:"bic"`
	CreatedAt        time.Time `json:"created_at"`
}

// UserPreferences stores per-user app settings (language, dark mode, biometrics).
// Legacy name kept as an alias of the generated UserPreference (singular).
type UserPreferences = UserPreference

// FAQ is an admin-managed frequently-asked-question entry.
// Hand-written counterpart of the generated Faq.
type FAQ struct {
	ID        uuid.UUID `json:"id"`
	Question  string    `json:"question"`
	Answer    string    `json:"answer"`
	Category  string    `json:"category"`
	SortOrder int       `json:"sort_order"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

// SupportTicketWithMessages is the full view returned when fetching a single ticket.
type SupportTicketWithMessages struct {
	SupportTicket
	Messages []SupportMessage `json:"messages"`
}
