package kyc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/rachfinance/digitalfx/internal/clients/metamap"
)

type MetaMapProvider struct {
	client        *metamap.Client
	flowID        string
	webhookSecret string
}

func NewMetaMapProvider(client *metamap.Client, flowID, webhookSecret string) *MetaMapProvider {
	return &MetaMapProvider{
		client:        client,
		flowID:        flowID,
		webhookSecret: webhookSecret,
	}
}

func (p *MetaMapProvider) Name() string {
	return "metamap"
}

func (p *MetaMapProvider) Initiate(ctx context.Context, userID, phone, email string) (*VerificationSession, error) {
	resp, err := p.client.CreateApplicant(ctx, metamap.CreateApplicantRequest{
		Metadata: metamap.ApplicantMetadata{
			UserID: userID,
			Phone:  phone,
			Email:  email,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("metamap provider: %w", err)
	}

	return &VerificationSession{
		ExternalID:  resp.ID,
		AccessToken: resp.IdentityAccess,
		FlowID:      p.flowID,
		Status:      "pending",
	}, nil
}

func (p *MetaMapProvider) HandleWebhook(ctx context.Context, body []byte, headers http.Header) (*VerificationEvent, error) {
	signature := headers.Get("X-Signature")
	
	// Metamap uses HMAC-SHA256 of the body with webhook secret
	mac := hmac.New(sha256.New, []byte(p.webhookSecret))
	mac.Write(body)
	expectedMAC := hex.EncodeToString(mac.Sum(nil))

	if signature != expectedMAC && p.webhookSecret != "" {
		return nil, fmt.Errorf("metamap provider: invalid webhook signature")
	}

	var payload metamap.WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("metamap provider: unmarshal webhook: %w", err)
	}

	applicantID := metamap.ApplicantIDFromResource(payload.Resource)
	if applicantID == "" {
		return nil, fmt.Errorf("metamap provider: empty applicant id")
	}

	var meta struct {
		UserID string `json:"userId"`
	}
	_ = json.Unmarshal(payload.Metadata, &meta)

	var status string
	switch payload.EventName {
	case "verification_completed", "step_completed":
		status = "under_review"
	case "verification_rejected", "step_rejected":
		status = "rejected"
	case "verification_started", "step_started":
		status = "processing"
	default:
		status = "pending"
	}

	return &VerificationEvent{
		ExternalID: applicantID,
		UserID:     meta.UserID,
		Status:     status,
		RawPayload: body,
	}, nil
}