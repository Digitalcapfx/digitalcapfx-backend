package kyc

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/rachfinance/digitalfx/internal/clients/didit"
)

const testSecret = "test-webhook-secret"

// v3Body builds a real Didit V3 session webhook envelope. V3 is flat, uses
// "webhook_type", and sends unix INTEGER timestamps.
func v3Body(status string, ts int64, vendorData string) []byte {
	return []byte(fmt.Sprintf(`{
  "event_id": "11111111-2222-3333-4444-555555555555",
  "webhook_type": "status.updated",
  "timestamp": %d,
  "created_at": %d,
  "application_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  "session_id": "4c5c7f3a-0000-0000-0000-000000000000",
  "status": %q,
  "workflow_id": "ca0c9e48-9723-41a4-a5c0-be98577c65c4",
  "workflow_version": 4,
  "vendor_data": %q,
  "decision": {"id_verifications": [{"first_name": "Ada", "score": 99.0}]}
}`, ts, ts-6, status, vendorData))
}

// signV2 reproduces Didit's X-Signature-V2: HMAC-SHA256 over the canonicalised
// body (whole-number floats shortened, keys sorted, re-serialised compactly).
func signV2(t *testing.T, secret string, body []byte) string {
	t.Helper()
	var v any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("canonicalise: %v", err)
	}
	canonical, err := json.Marshal(shorten(v))
	if err != nil {
		t.Fatalf("marshal canonical: %v", err)
	}
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(canonical)
	return hex.EncodeToString(h.Sum(nil))
}


func shorten(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = shorten(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = shorten(val)
		}
		return out
	case json.Number:
		s := t.String()
		if !containsAny(s, ".eE") {
			return t
		}
		if f, err := t.Float64(); err == nil && f == float64(int64(f)) {
			return json.Number(fmt.Sprintf("%d", int64(f)))
		}
		return t
	default:
		return v
	}
}

func containsAny(s, chars string) bool {
	for _, c := range chars {
		for _, x := range s {
			if x == c {
				return true
			}
		}
	}
	return false
}

func newProvider(secret string) *DiditProvider {
	return NewDiditProvider(didit.New("", "api-key-present", secret, ""))
}

func signedHeaders(t *testing.T, body []byte) http.Header {
	t.Helper()
	h := http.Header{}
	h.Set("X-Signature-V2", signV2(t, testSecret, body))
	return h
}

// ─── The bug that silently dropped every delivery ────────────────────────────

func TestRealV3EnvelopeIsAccepted(t *testing.T) {
	p := newProvider(testSecret)
	body := v3Body("Approved", time.Now().Unix(), "user-uuid-123")

	ev, err := p.HandleWebhook(context.Background(), body, signedHeaders(t, body))
	if err != nil {
		t.Fatalf("real Didit V3 webhook rejected: %v", err)
	}
	if ev.UserID != "user-uuid-123" {
		t.Errorf("UserID = %q, want user-uuid-123", ev.UserID)
	}
	if ev.Status != "approved" || ev.ReviewAnswer != "GREEN" {
		t.Errorf("Status/ReviewAnswer = %q/%q, want approved/GREEN", ev.Status, ev.ReviewAnswer)
	}
	if ev.EventID == "" {
		t.Error("EventID empty — webhook retries could not be deduped")
	}
}

// ─── Status mapping: only a real decision may change the decision ────────────

func TestOnlyApprovedApproves(t *testing.T) {
	approving := map[string]bool{"Approved": true}
	all := []string{
		"Not Started", "In Progress", "Awaiting User", "In Review", "Approved",
		"Declined", "Resubmitted", "Abandoned", "Expired", "Kyc Expired",
	}
	for _, s := range all {
		canonical, _, _, _ := mapDiditStatus(s)
		if got := canonical == "approved"; got != approving[s] {
			t.Errorf("status %q -> canonical %q (approves=%v), want approves=%v", s, canonical, got, approving[s])
		}
	}
}

func TestUnknownStatusNeverApproves(t *testing.T) {
	for _, s := range []string{"", "approved", "APPROVED", "weird", "status.updated"} {
		if canonical, _, _, _ := mapDiditStatus(s); canonical == "approved" {
			t.Errorf("status %q approved the user — matching must be exact and case-sensitive", s)
		}
	}
}

func TestDeclinedAndExpiryRevoke(t *testing.T) {
	for _, s := range []string{"Declined", "Kyc Expired"} {
		canonical, identity, review, comment := mapDiditStatus(s)
		if canonical != "rejected" || identity != "rejected" || review != "RED" {
			t.Errorf("status %q = (%q,%q,%q), want rejected/rejected/RED", s, canonical, identity, review)
		}
		if comment == "" {
			t.Errorf("status %q has no user-facing reason", s)
		}
	}
}

func TestInReviewQueuesRatherThanApproves(t *testing.T) {
	canonical, identity, _, _ := mapDiditStatus("In Review")
	if canonical != "under_review" || identity != "in_review" {
		t.Errorf("In Review = (%q,%q), want under_review/in_review", canonical, identity)
	}
}

// ─── Signature verification ──────────────────────────────────────────────────

func TestUnsignedWebhookRejectedWhenSecretConfigured(t *testing.T) {
	p := newProvider(testSecret)
	body := v3Body("Approved", time.Now().Unix(), "victim")

	if _, err := p.HandleWebhook(context.Background(), body, http.Header{}); err == nil {
		t.Fatal("unsigned webhook accepted")
	}
}

// Fail closed: with no secret configured nothing may be trusted, or the endpoint
// becomes an unauthenticated way to approve any user by id.
func TestFailsClosedWithNoSecret(t *testing.T) {
	p := newProvider("")
	body := v3Body("Approved", time.Now().Unix(), "victim")

	if _, err := p.HandleWebhook(context.Background(), body, http.Header{}); err == nil {
		t.Fatal("webhook accepted with no secret configured — anyone could approve any user")
	}
}

func TestWrongSecretRejected(t *testing.T) {
	p := newProvider(testSecret)
	body := v3Body("Approved", time.Now().Unix(), "victim")
	h := http.Header{}
	h.Set("X-Signature-V2", signV2(t, "not-the-secret", body))

	if _, err := p.HandleWebhook(context.Background(), body, h); err == nil {
		t.Fatal("webhook signed with the wrong secret was accepted")
	}
}

func TestTamperedBodyRejected(t *testing.T) {
	p := newProvider(testSecret)
	genuine := v3Body("Declined", time.Now().Unix(), "user-uuid-123")
	h := signedHeaders(t, genuine)

	forged := v3Body("Approved", time.Now().Unix(), "user-uuid-123")
	if _, err := p.HandleWebhook(context.Background(), forged, h); err == nil {
		t.Fatal("body swapped from Declined to Approved under a genuine signature was accepted")
	}
}

// ─── Replay ──────────────────────────────────────────────────────────────────

func TestStaleWebhookRejected(t *testing.T) {
	p := newProvider(testSecret)
	old := time.Now().Add(-10 * time.Minute).Unix()
	body := v3Body("Approved", old, "user-uuid-123")

	if _, err := p.HandleWebhook(context.Background(), body, signedHeaders(t, body)); err == nil {
		t.Fatal("10-minute-old webhook accepted — replay window not enforced")
	}
}

// Freshness must come from the SIGNED body, not the unsigned X-Timestamp header,
// which an attacker can freshen on a captured (body, signature) pair.
func TestFreshHeaderCannotRescueStaleBody(t *testing.T) {
	p := newProvider(testSecret)
	old := time.Now().Add(-30 * time.Minute).Unix()
	body := v3Body("Approved", old, "user-uuid-123")
	h := signedHeaders(t, body)
	h.Set("X-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))

	if _, err := p.HandleWebhook(context.Background(), body, h); err == nil {
		t.Fatal("stale body accepted because the unsigned header was fresh")
	}
}

// ─── Attribution ─────────────────────────────────────────────────────────────

func TestWebhookWithoutVendorDataIsRejected(t *testing.T) {
	p := newProvider(testSecret)
	body := v3Body("Approved", time.Now().Unix(), "")

	if _, err := p.HandleWebhook(context.Background(), body, signedHeaders(t, body)); err == nil {
		t.Fatal("webhook with no vendor_data accepted — it cannot be attributed to a user")
	}
}

// ─── Session creation ────────────────────────────────────────────────────────

// Without an API key there is no way to mint a per-user session. Initiate must
// fail loudly rather than hand back a shared link that carries no vendor_data.
func TestInitiateFailsWithoutAPIKey(t *testing.T) {
	p := NewDiditProvider(didit.New("", "", testSecret, ""))

	if _, err := p.Initiate(context.Background(), "user-uuid-1234", "+237699999999", "a@b.com"); err == nil {
		t.Fatal("Initiate succeeded with no API key — it must refuse, not fall back to a shared link")
	}
}

func TestInitiateRefusesEmptyUserID(t *testing.T) {
	p := newProvider(testSecret)

	if _, err := p.Initiate(context.Background(), "", "", ""); err == nil {
		t.Fatal("Initiate accepted an empty vendor_data")
	}
}
