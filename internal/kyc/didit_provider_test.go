package kyc

import (
	"context"
	"net/http"
	"testing"

	"github.com/rachfinance/digitalfx/internal/clients/didit"
)

func TestMapDiditStatus(t *testing.T) {
	cases := []struct {
		status         string
		event          string
		wantCanonical  string
		wantIdentity   string
		wantReviewAns  string
	}{
		{"Approved", "status.updated", "approved", "approved", "GREEN"},
		{"In Review", "status.updated", "approved", "approved", "GREEN"},
		{"declined", "status.updated", "rejected", "rejected", "RED"},
		{"Rejected", "status.updated", "rejected", "rejected", "RED"},
		{"Abandoned", "status.updated", "under_review", "in_progress", ""},
		{"In Progress", "status.updated", "approved", "approved", "GREEN"},
	}

	for _, c := range cases {
		canon, ident, ans := mapDiditStatus(c.status, c.event)
		if canon != c.wantCanonical || ident != c.wantIdentity || ans != c.wantReviewAns {
			t.Errorf("mapDiditStatus(%q, %q) = (%q, %q, %q), want (%q, %q, %q)",
				c.status, c.event, canon, ident, ans, c.wantCanonical, c.wantIdentity, c.wantReviewAns)
		}
	}
}

func TestDiditProviderInitiate(t *testing.T) {
	client := didit.New("ca0c9e48-9723-41a4-a5c0-be98577c65c4", "https://verify.didit.me/u/ygyeSJcjQaSlwL6YV3xlxA", "", "")
	p := NewDiditProvider(client)

	sess, err := p.Initiate(context.Background(), "user-uuid-1234", "+237699999999", "test@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sess.FlowID != "ca0c9e48-9723-41a4-a5c0-be98577c65c4" {
		t.Errorf("FlowID = %q, want ca0c9e48-9723-41a4-a5c0-be98577c65c4", sess.FlowID)
	}

	if sess.AccessToken == "" {
		t.Errorf("expected AccessToken with verification URL, got empty")
	}
}

func TestDiditProviderWebhook(t *testing.T) {
	client := didit.New("ca0c9e48-9723-41a4-a5c0-be98577c65c4", "https://verify.didit.me/u/ygyeSJcjQaSlwL6YV3xlxA", "", "")
	p := NewDiditProvider(client)

	payload := []byte(`{
		"event": "status.updated",
		"application_id": "ca0c9e48-9723-41a4-a5c0-be98577c65c4",
		"timestamp": "2026-09-30T12:00:00Z",
		"data": {
			"session_id": "sess_123",
			"vendor_data": "user-uuid-123",
			"status": "Approved"
		}
	}`)

	headers := http.Header{}
	event, err := p.HandleWebhook(context.Background(), payload, headers)
	if err != nil {
		t.Fatalf("HandleWebhook error: %v", err)
	}

	if event.UserID != "user-uuid-123" {
		t.Errorf("UserID = %q, want user-uuid-123", event.UserID)
	}
	if event.Status != "approved" {
		t.Errorf("Status = %q, want approved", event.Status)
	}
	if event.ReviewAnswer != "GREEN" {
		t.Errorf("ReviewAnswer = %q, want GREEN", event.ReviewAnswer)
	}
}
