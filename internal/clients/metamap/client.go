package metamap

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const defaultBaseURL = "https://api.getmati.com"

type Client struct {
	clientID     string
	clientSecret string
	flowID       string
	baseURL      string
	httpClient   *http.Client

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func New(clientID, clientSecret, flowID string) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		flowID:       flowID,
		baseURL:      defaultBaseURL,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

// ─── OAuth ────────────────────────────────────────────────────────────────────

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func (c *Client) ensureToken(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Now().Before(c.tokenExpiry) {
		return nil
	}

	url := c.baseURL + "/oauth?grant_type=client_credentials"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("metamap: token request: %w", err)
	}

	creds := base64.StdEncoding.EncodeToString([]byte(c.clientID + ":" + c.clientSecret))
	req.Header.Set("Authorization", "Basic "+creds)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("metamap: token fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("metamap: token status %d", resp.StatusCode)
	}

	var t tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return fmt.Errorf("metamap: decode token: %w", err)
	}

	c.accessToken = t.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(t.ExpiresIn-60) * time.Second)
	return nil
}

// ─── Create Applicant ─────────────────────────────────────────────────────────

type ApplicantMetadata struct {
	UserID string `json:"userId"`
	Phone  string `json:"phone"`
	Email  string `json:"email,omitempty"`
}

type CreateApplicantRequest struct {
	Metadata ApplicantMetadata `json:"metadata"`
}

type CreateApplicantResponse struct {
	ID             string `json:"_id"`
	IdentityAccess string `json:"identityAccess"`
}

func (c *Client) CreateApplicant(ctx context.Context, req CreateApplicantRequest) (*CreateApplicantResponse, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}

	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v2/applicants", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("metamap: create applicant request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.accessToken)
	httpReq.Header.Set("X-Verification-Flow-Id", c.flowID)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("metamap: create applicant: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metamap: create applicant status %d", resp.StatusCode)
	}

	var out CreateApplicantResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("metamap: decode applicant: %w", err)
	}
	return &out, nil
}

// ─── Get Applicant ────────────────────────────────────────────────────────────

type ApplicantStatus struct {
	ID     string `json:"_id"`
	Status string `json:"status"`
}

func (c *Client) GetApplicant(ctx context.Context, applicantID string) (*ApplicantStatus, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}

	url := c.baseURL + "/v2/applicants/" + applicantID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metamap: get applicant: %w", err)
	}
	defer resp.Body.Close()

	var out ApplicantStatus
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ─── Webhook helpers ──────────────────────────────────────────────────────────

// WebhookPayload is the top-level structure MetaMap POSTs to our callback URL.
type WebhookPayload struct {
	EventName string          `json:"eventName"`
	Resource  string          `json:"resource"`  // URL containing the applicant ID
	Metadata  json.RawMessage `json:"metadata"`
	Status    json.RawMessage `json:"status"`
}

// ApplicantIDFromResource extracts the applicant ID from the resource URL
// e.g. "https://api.getmati.com/v2/applicants/60abc..." → "60abc..."
func ApplicantIDFromResource(resource string) string {
	parts := strings.Split(resource, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}
