package hub2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client wraps the HUB2 API for XAF/XOF Mobile Money collection and disbursement.
// Docs: https://docs.hub2.io
type Client struct {
	baseURL    string
	apiKey     string
	secretKey  string
	mode       string // sandbox | production
	httpClient *http.Client
}

func NewClient(baseURL, apiKey, secretKey, mode string) *Client {
	return &Client{
		baseURL:   baseURL,
		apiKey:    apiKey,
		secretKey: secretKey,
		mode:      mode,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ── Request / Response types ─────────────────────────────────────────────────

type CollectRequest struct {
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`  // XAF | XOF
	Phone       string  `json:"phone"`
	Operator    string  `json:"operator"`  // Orange | MTN | Wave | Moov | Airtel
	Description string  `json:"description,omitempty"`
	CallbackURL string  `json:"callbackUrl,omitempty"`
}

type CollectResponse struct {
	Reference string `json:"reference"`
	Status    string `json:"status"`
}

type DisburseRequest struct {
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	Phone       string  `json:"phone"`
	Operator    string  `json:"operator"`
	Description string  `json:"description,omitempty"`
}

type DisburseResponse struct {
	Reference string `json:"reference"`
	Status    string `json:"status"`
}

type WebhookPayload struct {
	Reference string `json:"reference"`
	Status    string `json:"status"`   // SUCCESSFUL | FAILED | CANCELLED | PENDING
	Amount    float64 `json:"amount"`
	Currency  string  `json:"currency"`
	Phone     string  `json:"phone"`
}

// ── Methods ──────────────────────────────────────────────────────────────────

// Collect initiates a Mobile Money payment request (pull from customer).
func (c *Client) Collect(ctx context.Context, req CollectRequest) (*CollectResponse, error) {
	var resp CollectResponse
	if err := c.post(ctx, "/v1/payment-requests", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Disburse sends money to a Mobile Money account (push to customer).
func (c *Client) Disburse(ctx context.Context, req DisburseRequest) (*DisburseResponse, error) {
	var resp DisburseResponse
	if err := c.post(ctx, "/v1/disbursements", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetStatus checks the current status of a payment by reference.
func (c *Client) GetStatus(ctx context.Context, reference string) (string, error) {
	var resp struct {
		Status string `json:"status"`
	}
	if err := c.get(ctx, fmt.Sprintf("/v1/payment-requests/%s", reference), &resp); err != nil {
		return "", err
	}
	return resp.Status, nil
}

// ── HTTP helpers ─────────────────────────────────────────────────────────────

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("x-mode", c.mode)
	return c.do(req, out)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("x-mode", c.mode)
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errBody map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return fmt.Errorf("hub2 api %d: %v", resp.StatusCode, errBody)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
