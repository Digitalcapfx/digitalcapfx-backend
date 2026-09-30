package didit

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultWorkflowID = "ca0c9e48-9723-41a4-a5c0-be98577c65c4"
	DefaultVerifyURL  = "https://verify.didit.me/u/ygyeSJcjQaSlwL6YV3xlxA"
	DefaultAPIBaseURL = "https://verification.didit.me"
)

type Client struct {
	workflowID    string
	verifyURL     string
	apiKey        string
	webhookSecret string
	baseURL       string
	httpClient    *http.Client
}

func New(workflowID, verifyURL, apiKey, webhookSecret string) *Client {
	if workflowID == "" {
		workflowID = DefaultWorkflowID
	}
	if verifyURL == "" {
		verifyURL = DefaultVerifyURL
	}
	return &Client{
		workflowID:    workflowID,
		verifyURL:     verifyURL,
		apiKey:        apiKey,
		webhookSecret: webhookSecret,
		baseURL:       DefaultAPIBaseURL,
		httpClient:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) WorkflowID() string {
	return c.workflowID
}

func (c *Client) VerifyURL() string {
	return c.verifyURL
}

type CreateSessionRequest struct {
	WorkflowID string `json:"workflow_id"`
	VendorData string `json:"vendor_data,omitempty"`
	Callback   string `json:"callback,omitempty"`
}

type CreateSessionResponse struct {
	SessionID string `json:"session_id"`
	URL       string `json:"url"`
}

// CreateSession generates a verification session for the user.
// If an API key is configured, it calls the Didit API to create an authenticated session.
// Otherwise, it returns the hosted Free KYC verification link tagged with vendor_data.
func (c *Client) CreateSession(ctx context.Context, userID, email, phone string) (sessionID string, verifyURL string, err error) {
	if c.apiKey != "" {
		reqBody := CreateSessionRequest{
			WorkflowID: c.workflowID,
			VendorData: userID,
		}
		data, mErr := json.Marshal(reqBody)
		if mErr == nil {
			req, rErr := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v3/session/", bytes.NewReader(data))
			if rErr == nil {
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("x-api-key", c.apiKey)
				req.Header.Set("Accept", "application/json")

				resp, doErr := c.httpClient.Do(req)
				if doErr == nil {
					defer resp.Body.Close()
					if resp.StatusCode >= 200 && resp.StatusCode < 300 {
						var sResp CreateSessionResponse
						if decErr := json.NewDecoder(resp.Body).Decode(&sResp); decErr == nil {
							if sResp.URL != "" {
								return sResp.SessionID, sResp.URL, nil
							}
						}
					}
				}
			}
		}
	}

	// Fallback / default to direct hosted Free KYC link with vendor_data
	parsed, parseErr := url.Parse(c.verifyURL)
	if parseErr != nil {
		return userID, fmt.Sprintf("%s?vendor_data=%s", c.verifyURL, url.QueryEscape(userID)), nil
	}
	q := parsed.Query()
	q.Set("vendor_data", userID)
	if email != "" {
		q.Set("email", email)
	}
	parsed.RawQuery = q.Encode()

	return userID, parsed.String(), nil
}

// WebhookData represents the data payload of a Didit webhook.
type WebhookData struct {
	SessionID      string `json:"session_id"`
	SessionKind    string `json:"session_kind"`
	VendorData     string `json:"vendor_data"`
	Status         string `json:"status"`
	PreviousStatus string `json:"previous_status"`
	Email          string `json:"email,omitempty"`
}

// WebhookPayload represents the envelope of incoming Didit webhook events.
type WebhookPayload struct {
	Event         string       `json:"event"`
	ApplicationID string       `json:"application_id"`
	Timestamp     string       `json:"timestamp"`
	Data          *WebhookData `json:"data,omitempty"`

	// Flat field fallbacks in case payload is flat
	SessionID  string `json:"session_id,omitempty"`
	VendorData string `json:"vendor_data,omitempty"`
	Status     string `json:"status,omitempty"`
}

// VerifyWebhookSignature verifies HMAC-SHA256 signature from Didit webhook headers (X-Signature-V2).
func (c *Client) VerifyWebhookSignature(signature string, body []byte) bool {
	// If no secret configured, accept webhook for easy setup
	if c.webhookSecret == "" {
		return true
	}
	if signature == "" {
		return false
	}
	h := hmac.New(sha256.New, []byte(c.webhookSecret))
	h.Write(body)
	expected := hex.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(strings.ToLower(signature)), []byte(strings.ToLower(expected)))
}
