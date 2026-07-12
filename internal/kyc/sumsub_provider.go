package kyc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/rachfinance/digitalfx/internal/clients/sumsub"
)

type SumsubProvider struct {
	client *sumsub.Client
}

func NewSumsubProvider(client *sumsub.Client) *SumsubProvider {
	return &SumsubProvider{client: client}
}

func (p *SumsubProvider) Name() string {
	return "sumsub"
}

func (p *SumsubProvider) Initiate(ctx context.Context, userID, phone, email string) (*VerificationSession, error) {
	// For sumsub, the applicantID/externalUserID is mapped directly to our userID.
	// Generate an access token valid for 30 minutes (1800 seconds).
	tokenResp, err := p.client.GenerateAccessToken(ctx, userID, p.client.LevelName(), 1800)
	if err != nil {
		return nil, fmt.Errorf("sumsub provider: %w", err)
	}

	return &VerificationSession{
		ExternalID:  userID, // We use our userID as the external ID in Sumsub
		AccessToken: tokenResp.Token,
		FlowID:      p.client.LevelName(),
		Status:      "pending", // initial status
	}, nil
}

func (p *SumsubProvider) HandleWebhook(ctx context.Context, body []byte, headers http.Header) (*VerificationEvent, error) {
	signature := headers.Get("X-Payload-Digest")
	if signature == "" {
		signature = headers.Get("X-App-Access-Sig") // Fallback if they use the standard signing header on webhooks
	}
	alg := headers.Get("X-Payload-Digest-Alg")

	if !p.client.VerifyWebhookSignature(signature, alg, body) {
		return nil, fmt.Errorf("sumsub provider: invalid webhook signature")
	}

	var payload sumsub.WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("sumsub provider: unmarshal webhook: %w", err)
	}

	// Sumsub uses ExternalUserId to mirror our internal userID.
	if payload.ExternalUserId == "" {
		return nil, fmt.Errorf("sumsub provider: empty externalUserId in webhook payload")
	}

	status := mapSumsubEvent(payload.Type, payload.ReviewResult.ReviewAnswer)

	return &VerificationEvent{
		ExternalID: payload.ApplicantId,
		UserID:     payload.ExternalUserId,
		Status:     status,
		RawPayload: body,
	}, nil
}

func mapSumsubEvent(eventType, reviewAnswer string) string {
	switch eventType {
	case "applicantReviewed":
		if reviewAnswer == "GREEN" {
			return "approved"
		}
		return "rejected"
	case "applicantPending", "applicantCreated":
		return "under_review"
	case "applicantActionPending":
		return "processing"
	default:
		return "pending"
	}
}
