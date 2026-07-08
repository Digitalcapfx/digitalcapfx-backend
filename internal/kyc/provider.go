package kyc

import (
	"context"
	"net/http"
)

// VerificationSession represents the data needed by mobile/frontend clients
// to launch a specific KYC vendor's SDK.
type VerificationSession struct {
	ExternalID  string // The provider's applicant or session ID
	AccessToken string // SDK access token for the client
	FlowID      string // Flow or level name
	Status      string // Our internal status: pending, processing, under_review, approved, rejected
}

// VerificationEvent is a normalized event emitted by a vendor's webhook.
type VerificationEvent struct {
	ExternalID string // Provider's applicant or review ID
	UserID     string // Our internal user ID (parsed from metadata)
	Status     string // Normalized status: pending, processing, under_review, approved, rejected
	RawPayload []byte // Full webhook body for audit or debugging
}

// KYCProvider is the abstraction that all KYC vendors must implement.
type KYCProvider interface {
	// Initiate creates or retrieves a verification session for the user.
	// The implementation must handle any API calls required to get an SDK access token.
	Initiate(ctx context.Context, userID, phone, email string) (*VerificationSession, error)

	// HandleWebhook parses, validates, and normalizes an incoming webhook payload.
	// It must verify signatures or HMACs using the provided headers.
	HandleWebhook(ctx context.Context, body []byte, headers http.Header) (*VerificationEvent, error)

	// Name returns the provider identifier ("metamap" or "sumsub").
	Name() string
}
