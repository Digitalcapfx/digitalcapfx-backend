package didit

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// "Free KYC" — the account's default workflow. See config.go for why its
	// paid add-ons must stay off.
	DefaultWorkflowID = "ca0c9e48-9723-41a4-a5c0-be98577c65c4"
	DefaultAPIBaseURL = "https://verification.didit.me"

	// Didit rejects a webhook older than 5 minutes; we enforce the same window
	// so a captured delivery cannot be replayed later.
	WebhookMaxAge = 5 * time.Minute
)

type Client struct {
	workflowID    string
	apiKey        string
	webhookSecret string
	callbackURL   string
	baseURL       string
	httpClient    *http.Client
}

func New(workflowID, apiKey, webhookSecret, callbackURL string) *Client {
	if workflowID == "" {
		workflowID = DefaultWorkflowID
	}
	return &Client{
		workflowID:    workflowID,
		apiKey:        apiKey,
		webhookSecret: webhookSecret,
		callbackURL:   callbackURL,
		baseURL:       DefaultAPIBaseURL,
		httpClient:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) WorkflowID() string { return c.workflowID }

// Configured reports whether a session can actually be created. Without an API
// key there is no way to mint a per-user session, and a verification that is not
// tied to a user is worthless — so callers must refuse rather than degrade.
func (c *Client) Configured() bool { return c != nil && strings.TrimSpace(c.apiKey) != "" }

type createSessionRequest struct {
	WorkflowID string `json:"workflow_id"`
	VendorData string `json:"vendor_data"`
	Callback   string `json:"callback,omitempty"`
}

type createSessionResponse struct {
	SessionID string `json:"session_id"`
	URL       string `json:"url"`
	Status    string `json:"status"`
}

// CreateSession mints a verification session bound to this user via vendor_data.
//
// Every failure is returned. There is deliberately no fallback to a shared hosted
// link: that link carries no vendor_data, so the decision webhook could not be
// attributed to a user and the verification would be silently meaningless.
func (c *Client) CreateSession(ctx context.Context, userID, email, phone string) (sessionID, verifyURL string, err error) {
	if !c.Configured() {
		return "", "", fmt.Errorf("didit: DIDIT_API_KEY is not set, cannot create a verification session")
	}
	if strings.TrimSpace(userID) == "" {
		return "", "", fmt.Errorf("didit: refusing to create a session with empty vendor_data")
	}

	body, err := json.Marshal(createSessionRequest{
		WorkflowID: c.workflowID,
		VendorData: userID,
		Callback:   c.callbackURL,
	})
	if err != nil {
		return "", "", fmt.Errorf("didit: marshal create-session: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v3/session/", bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("didit: build create-session request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("didit: create-session request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 403 means the API key is missing, wrong, or revoked.
		return "", "", fmt.Errorf("didit: create-session returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var out createSessionResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", fmt.Errorf("didit: decode create-session response: %w", err)
	}
	if out.URL == "" {
		return "", "", fmt.Errorf("didit: create-session returned no verification url")
	}
	return out.SessionID, out.URL, nil
}

// WebhookPayload is the Didit V3 session webhook envelope.
//
// V3 is FLAT (no "data" wrapper), carries "webhook_type" rather than "event",
// and sends timestamps as unix INTEGERS. Modelling any of those three wrongly
// makes json.Unmarshal fail and drops every delivery.
type WebhookPayload struct {
	EventID       string          `json:"event_id"`
	WebhookType   string          `json:"webhook_type"`
	Timestamp     int64           `json:"timestamp"`
	CreatedAt     int64           `json:"created_at"`
	ApplicationID string          `json:"application_id"`
	SessionID     string          `json:"session_id"`
	Status        string          `json:"status"`
	WorkflowID    string          `json:"workflow_id"`
	VendorData    string          `json:"vendor_data"`
	Decision      json.RawMessage `json:"decision,omitempty"`
	ResubmitInfo  *ResubmitInfo   `json:"resubmit_info,omitempty"`
}

type ResubmitInfo struct {
	NodesToResubmit []string          `json:"nodes_to_resubmit"`
	Reasons         map[string]string `json:"reasons"`
}

// VerifyWebhookSignature authenticates a delivery.
//
// Fails CLOSED: with no secret configured nothing is trusted. Treating an
// unsigned request as genuine would expose an unauthenticated endpoint that
// approves any user by id.
//
// X-Signature-V2 signs the CANONICALISED body (whole-number floats shortened,
// keys sorted, re-serialised); X-Signature signs the raw bytes. Both are
// accepted — each is an HMAC with the same secret over a deterministic
// rendering of the same payload, so honouring both costs nothing and survives
// middleware that re-encodes JSON.
func (c *Client) VerifyWebhookSignature(signatureV2, signatureRaw string, body []byte) bool {
	if strings.TrimSpace(c.webhookSecret) == "" {
		return false
	}
	if canonical, err := canonicalJSON(body); err == nil {
		if equalHMAC(c.webhookSecret, canonical, signatureV2) {
			return true
		}
	}
	return equalHMAC(c.webhookSecret, body, signatureRaw)
}

// IsFresh enforces the 300s replay window against the SIGNED body timestamp.
// The X-Timestamp header is excluded from the HMAC, so a replayed (body,
// signature) pair with a freshened header would otherwise pass.
func IsFresh(ts int64, now time.Time) bool {
	if ts <= 0 {
		return false
	}
	delta := now.Sub(time.Unix(ts, 0))
	if delta < 0 {
		delta = -delta
	}
	return delta <= WebhookMaxAge
}

func equalHMAC(secret string, msg []byte, signature string) bool {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return false
	}
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(msg)
	expected := hex.EncodeToString(h.Sum(nil))
	// Compare the decoded bytes so the comparison is constant time and not
	// sensitive to hex casing.
	got, err := hex.DecodeString(strings.ToLower(signature))
	if err != nil {
		return false
	}
	want, _ := hex.DecodeString(expected)
	return hmac.Equal(got, want)
}

// canonicalJSON reproduces Didit's X-Signature-V2 canonicalisation:
// shortenFloats -> sortKeys -> serialise with unescaped Unicode and no spaces.
func canonicalJSON(body []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber() // keep the literal number text; float64 would reformat it
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // JSON.stringify leaves < > & alone
	if err := enc.Encode(shortenFloats(v)); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// shortenFloats rewrites whole-number floats (1.0) as integers (1), matching
// Didit's server-side canonicalisation. Key ordering is handled by
// encoding/json, which always marshals map keys in sorted order.
func shortenFloats(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = shortenFloats(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = shortenFloats(val)
		}
		return out
	case json.Number:
		s := t.String()
		if !strings.ContainsAny(s, ".eE") {
			// Integer literal: keep verbatim. Round-tripping it through float64
			// would lose precision on ids larger than 2^53.
			return t
		}
		// Any non-integer literal is renormalised the way JSON.parse +
		// JSON.stringify would render it, so 99.0 -> 99 AND 2.50 -> 2.5.
		// Preserving the literal text here breaks the signature on every
		// payload containing a trailing-zero float.
		f, err := t.Float64()
		if err != nil {
			return t
		}
		return json.Number(strconv.FormatFloat(f, 'f', -1, 64))
	default:
		return v
	}
}
