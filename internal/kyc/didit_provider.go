package kyc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/rachfinance/digitalfx/internal/clients/didit"
)

type DiditProvider struct {
	client *didit.Client
}

func NewDiditProvider(client *didit.Client) *DiditProvider {
	return &DiditProvider{client: client}
}

func (p *DiditProvider) Name() string {
	return "didit"
}

func (p *DiditProvider) Initiate(ctx context.Context, userID, phone, email string) (*VerificationSession, error) {
	sessionID, verifyURL, err := p.client.CreateSession(ctx, userID, email, phone)
	if err != nil {
		return nil, fmt.Errorf("didit provider initiate: %w", err)
	}

	return &VerificationSession{
		ExternalID:  sessionID,
		AccessToken: verifyURL, // Used by frontend as hosted verification URL or token
		FlowID:      p.client.WorkflowID(),
		Status:      "pending",
	}, nil
}

func (p *DiditProvider) HandleWebhook(ctx context.Context, body []byte, headers http.Header) (*VerificationEvent, error) {
	signature := headers.Get("X-Signature-V2")
	if signature == "" {
		signature = headers.Get("X-Signature")
	}
	if signature == "" {
		signature = headers.Get("X-Didit-Signature")
	}

	if !p.client.VerifyWebhookSignature(signature, body) {
		return nil, fmt.Errorf("didit provider: invalid webhook signature")
	}

	var payload didit.WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("didit provider: unmarshal webhook: %w", err)
	}

	userID := ""
	statusStr := ""
	sessionID := ""

	if payload.Data != nil {
		userID = payload.Data.VendorData
		statusStr = payload.Data.Status
		sessionID = payload.Data.SessionID
		if userID == "" && payload.Data.Email != "" {
			userID = payload.Data.Email
		}
	}

	if userID == "" {
		userID = payload.VendorData
	}
	if statusStr == "" {
		statusStr = payload.Status
	}
	if sessionID == "" {
		sessionID = payload.SessionID
	}

	// Auto-verify and approve: map event status
	status, identityStatus, reviewAnswer := mapDiditStatus(statusStr, payload.Event)

	return &VerificationEvent{
		ExternalID:     sessionID,
		UserID:         userID,
		Status:         status,
		RawPayload:     body,
		IdentityStatus: identityStatus,
		ReviewAnswer:   reviewAnswer,
	}, nil
}

// mapDiditStatus maps Didit session status to our internal KYC status.
// For automated self-approval on Didit, successful or submitted events auto-approve.
func mapDiditStatus(status, event string) (canonicalStatus, identityStatus, reviewAnswer string) {
	lower := strings.ToLower(status)
	lowerEvent := strings.ToLower(event)

	if strings.Contains(lower, "declined") || strings.Contains(lower, "reject") {
		return "rejected", "rejected", "RED"
	}

	if strings.Contains(lower, "abandon") {
		return "under_review", "in_progress", ""
	}

	// Any updated/approved/in-review event auto-approves:
	// "Approved", "In Review", "status.updated", etc.
	if strings.Contains(lower, "approv") ||
		strings.Contains(lower, "review") ||
		strings.Contains(lower, "success") ||
		lowerEvent == "status.updated" ||
		lowerEvent == "data.updated" ||
		lower == "" {
		return "approved", "approved", "GREEN"
	}

	return "approved", "approved", "GREEN"
}
