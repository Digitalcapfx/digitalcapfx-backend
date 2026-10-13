package kyc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rachfinance/digitalfx/internal/clients/didit"
)

type DiditProvider struct {
	client *didit.Client
	now    func() time.Time // injectable for tests
}

func NewDiditProvider(client *didit.Client) *DiditProvider {
	return &DiditProvider{client: client, now: time.Now}
}

func (p *DiditProvider) Name() string { return "didit" }

func (p *DiditProvider) Initiate(ctx context.Context, userID, phone, email string) (*VerificationSession, error) {
	sessionID, verifyURL, err := p.client.CreateSession(ctx, userID, email, phone)
	if err != nil {
		return nil, fmt.Errorf("didit provider initiate: %w", err)
	}

	return &VerificationSession{
		ExternalID:  sessionID,
		AccessToken: verifyURL, // hosted verification URL for the client to open
		FlowID:      p.client.WorkflowID(),
		Status:      "pending",
	}, nil
}

func (p *DiditProvider) HandleWebhook(ctx context.Context, body []byte, headers http.Header) (*VerificationEvent, error) {
	if !p.client.VerifyWebhookSignature(
		headers.Get("X-Signature-V2"),
		headers.Get("X-Signature"),
		body,
	) {
		return nil, fmt.Errorf("didit provider: invalid webhook signature")
	}

	var payload didit.WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("didit provider: unmarshal webhook: %w", err)
	}

	// Freshness is checked against the SIGNED body timestamp, never the
	// unsigned X-Timestamp header, which a replayed request can freshen.
	if !didit.IsFresh(payload.Timestamp, p.now()) {
		return nil, fmt.Errorf("didit provider: stale webhook (timestamp %d)", payload.Timestamp)
	}

	if payload.VendorData == "" {
		return nil, fmt.Errorf("didit provider: webhook has no vendor_data, cannot attribute to a user (session %s)", payload.SessionID)
	}

	status, identityStatus, reviewAnswer, comment := mapDiditStatus(payload.Status)

	return &VerificationEvent{
		EventID:           payload.EventID,
		ExternalID:        payload.SessionID,
		UserID:            payload.VendorData,
		Status:            status,
		RawPayload:        body,
		IdentityStatus:    identityStatus,
		ReviewAnswer:      reviewAnswer,
		ModerationComment: comment,
	}, nil
}

// mapDiditStatus maps a Didit V3 session status to our internal KYC state.
//
// Matching is EXACT and case-sensitive against the documented literals. An
// unrecognised status yields an empty canonical status, which leaves the user's
// KYC decision untouched — the default must never be "approved", or a session
// that merely started would verify someone who uploaded nothing.
func mapDiditStatus(status string) (canonical, identity, reviewAnswer, comment string) {
	switch status {
	case "Approved":
		return "approved", "approved", "GREEN", ""
	case "Declined":
		return "rejected", "rejected", "RED", "Your verification was not approved by our identity provider."
	case "In Review":
		return "under_review", "in_review", "", ""
	case "Resubmitted":
		return "under_review", "resubmit", "", "Additional information was requested for your verification."
	case "In Progress", "Awaiting User":
		return "processing", "in_progress", "", ""
	case "Abandoned":
		return "", "in_progress", "", ""
	case "Not Started", "Expired":
		return "", "not_started", "", ""
	case "Kyc Expired":
		// A previously verified identity has aged out of the retention policy.
		// Revoking is the safe direction; the user re-verifies from scratch.
		return "rejected", "rejected", "RED", "Your identity verification has expired. Please verify again."
	default:
		return "", "", "", ""
	}
}
