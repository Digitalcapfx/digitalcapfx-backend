// Package nomba is a client for the Nomba API (https://developer.nomba.com).
//
// DigitalFX uses Nomba for Nigerian Naira (NGN) rails:
//   - Virtual accounts   — a real NGN bank account number per customer, funded by
//     bank transfer (inbound credits arrive via the payment_success webhook).
//   - Bank transfers      — NGN payouts to any Nigerian bank account.
//   - Bank lookup         — name-enquiry + bank-code list for building recipients.
//
// Auth is OAuth2 client-credentials: POST /v1/auth/token/issue returns a JWT
// access_token (+ refresh_token) with an expiry. The client caches the token and
// refreshes it automatically. Every request also carries the parent business
// account UUID in the `accountId` header.
package nomba

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Base URLs.
const (
	ProdBaseURL    = "https://api.nomba.com"
	SandboxBaseURL = "https://sandbox.nomba.com"
)

const defaultBaseURL = ProdBaseURL

// Client is the Nomba API client. It is safe for concurrent use.
type Client struct {
	baseURL      string
	clientID     string
	clientSecret string
	accountID    string // parent business account UUID (accountId header)
	httpClient   *http.Client

	mu           sync.Mutex
	accessToken  string
	refreshToken string
	tokenExpiry  time.Time
}

type Option func(*Client)

func WithBaseURL(u string) Option {
	return func(c *Client) {
		if u != "" {
			c.baseURL = strings.TrimRight(u, "/")
		}
	}
}
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.httpClient.Timeout = d } }
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpClient = h
		}
	}
}

// New creates a Nomba client. clientID / clientSecret / accountID come from the
// Nomba dashboard. Defaults to the production base URL; pass WithBaseURL(nomba.SandboxBaseURL)
// for sandbox.
func New(clientID, clientSecret, accountID string, opts ...Option) *Client {
	c := &Client{
		baseURL:      defaultBaseURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		accountID:    accountID,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Configured reports whether the client has the credentials needed to make calls.
func (c *Client) Configured() bool {
	return c.clientID != "" && c.clientSecret != "" && c.accountID != ""
}

// ─── Errors ───────────────────────────────────────────────────────────────────

// APIError is a non-2xx response from Nomba.
type APIError struct {
	StatusCode  int    `json:"-"`
	Code        string `json:"code"`
	Description string `json:"description"`
	Message     string `json:"message"`
	rawBody     string
}

func (e *APIError) Error() string {
	msg := e.Description
	if msg == "" {
		msg = e.Message
	}
	if msg == "" {
		msg = e.rawBody
	}
	return fmt.Sprintf("nomba %d (code %s): %s", e.StatusCode, e.Code, msg)
}

// ─── Auth ─────────────────────────────────────────────────────────────────────

type tokenRequest struct {
	GrantType    string `json:"grant_type"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

type tokenData struct {
	BusinessID   string `json:"businessId"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expiresAt"` // ISO 8601
}

type tokenEnvelope struct {
	Code        string    `json:"code"`
	Description string    `json:"description"`
	Data        tokenData `json:"data"`
}

// token returns a valid access token, obtaining or refreshing one if necessary.
// It refreshes ~60s before the reported expiry to avoid using a token mid-flight.
func (c *Client) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second)) {
		return c.accessToken, nil
	}

	req := tokenRequest{
		GrantType:    "client_credentials",
		ClientID:     c.clientID,
		ClientSecret: c.clientSecret,
	}
	if c.refreshToken != "" {
		req.GrantType = "refresh_token"
		req.RefreshToken = c.refreshToken
	}

	env, err := c.issueToken(ctx, req)
	if err != nil && req.GrantType == "refresh_token" {
		// Refresh failed (e.g. refresh token expired) — fall back to a fresh
		// client_credentials grant.
		env, err = c.issueToken(ctx, tokenRequest{
			GrantType:    "client_credentials",
			ClientID:     c.clientID,
			ClientSecret: c.clientSecret,
		})
	}
	if err != nil {
		return "", err
	}

	c.accessToken = env.Data.AccessToken
	c.refreshToken = env.Data.RefreshToken
	c.tokenExpiry = parseExpiry(env.Data.ExpiresAt)
	if c.accessToken == "" {
		return "", fmt.Errorf("nomba: token issue returned empty access_token (code %s)", env.Code)
	}
	return c.accessToken, nil
}

func (c *Client) issueToken(ctx context.Context, body tokenRequest) (*tokenEnvelope, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/auth/token/issue", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("accountId", c.accountID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nomba token issue: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, parseAPIError(resp.StatusCode, raw)
	}
	var env tokenEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("nomba token issue: decode: %w", err)
	}
	return &env, nil
}

// parseExpiry parses the ISO-8601 expiresAt. On failure it assumes a conservative
// 5-minute TTL so a fresh token is fetched again soon rather than trusted forever.
func parseExpiry(s string) time.Time {
	if s == "" {
		return time.Now().Add(5 * time.Minute)
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z", "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Now().Add(5 * time.Minute)
}

// ─── Virtual accounts ─────────────────────────────────────────────────────────

// CreateVirtualAccountRequest provisions a real NGN bank account for a customer.
// AccountRef must be 16-64 chars (our stable per-account reference). AccountName
// must be 8-64 chars (the holder's name). BVN is optional but improves KYC/limits.
type CreateVirtualAccountRequest struct {
	AccountRef     string  `json:"accountRef"`
	AccountName    string  `json:"accountName"`
	BVN            string  `json:"bvn,omitempty"`
	ExpiryDate     string  `json:"expiryDate,omitempty"` // "YYYY-MM-DD HH:MM:SS"
	ExpectedAmount float64 `json:"expectedAmount,omitempty"`
}

// VirtualAccount is a provisioned NGN bank account.
type VirtualAccount struct {
	CreatedAt         string `json:"createdAt"`
	AccountHolderID   string `json:"accountHolderId"`
	AccountRef        string `json:"accountRef"`
	BVN               string `json:"bvn"`
	AccountName       string `json:"accountName"`
	BankName          string `json:"bankName"`
	BankAccountNumber string `json:"bankAccountNumber"`
	BankAccountName   string `json:"bankAccountName"`
	Currency          string `json:"currency"`
	CallbackURL       string `json:"callbackUrl"`
	Expired           bool   `json:"expired"`
}

// CreateVirtualAccount provisions a NGN virtual bank account.
// POST /v1/accounts/virtual
func (c *Client) CreateVirtualAccount(ctx context.Context, req CreateVirtualAccountRequest) (*VirtualAccount, error) {
	var out VirtualAccount
	if err := c.doJSON(ctx, http.MethodPost, "/v1/accounts/virtual", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetVirtualAccount fetches a virtual account by its accountRef.
// GET /v1/accounts/virtual/{accountRef}
func (c *Client) GetVirtualAccount(ctx context.Context, accountRef string) (*VirtualAccount, error) {
	var out VirtualAccount
	if err := c.doJSON(ctx, http.MethodGet, "/v1/accounts/virtual/"+accountRef, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ─── Bank transfer (payout) ───────────────────────────────────────────────────

// BankTransferRequest is an outbound NGN payout to a Nigerian bank account.
type BankTransferRequest struct {
	Amount        float64 `json:"amount"`
	AccountNumber string  `json:"accountNumber"` // 10 digits
	AccountName   string  `json:"accountName"`
	BankCode      string  `json:"bankCode"`
	MerchantTxRef string  `json:"merchantTxRef"` // unique idempotency key
	SenderName    string  `json:"senderName,omitempty"`
	Narration     string  `json:"narration,omitempty"`
}

// TransferResult is the outcome of a bank transfer.
// Status is "SUCCESS" or "PENDING_BILLING" (monitor the payout webhook for the latter).
type TransferResult struct {
	ID          string  `json:"id"`
	Amount      string  `json:"amount"`
	Status      string  `json:"status"`
	Type        string  `json:"type"`
	Fee         float64 `json:"fee"`
	TimeCreated string  `json:"timeCreated"`
	Meta        struct {
		APIRRN    string `json:"api_rrn"`
		Narration string `json:"narration"`
		BankCode  string `json:"bankCode"`
		BankName  string `json:"bankName"`
	} `json:"meta"`
}

// BankTransfer sends NGN from the parent account to a Nigerian bank account.
// POST /v2/transfers/bank
func (c *Client) BankTransfer(ctx context.Context, req BankTransferRequest) (*TransferResult, error) {
	var out TransferResult
	if err := c.doJSON(ctx, http.MethodPost, "/v2/transfers/bank", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ─── Bank lookup ──────────────────────────────────────────────────────────────

// Bank is a Nigerian bank code + name.
type Bank struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// ListBanks returns the supported Nigerian bank codes and names. Callers should
// cache the result — the list rarely changes.
// GET /v1/transfers/banks
func (c *Client) ListBanks(ctx context.Context) ([]Bank, error) {
	var out struct {
		Results []Bank `json:"results"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/v1/transfers/banks", nil, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

// AccountLookupResult is the name-enquiry result for a bank account.
type AccountLookupResult struct {
	AccountNumber string `json:"accountNumber"`
	AccountName   string `json:"accountName"`
}

// LookupBankAccount resolves the account holder name for a bank account so the
// sender can confirm the recipient before transferring.
// POST /v1/transfers/bank/lookup
func (c *Client) LookupBankAccount(ctx context.Context, accountNumber, bankCode string) (*AccountLookupResult, error) {
	body := map[string]string{"accountNumber": accountNumber, "bankCode": bankCode}
	var out AccountLookupResult
	if err := c.doJSON(ctx, http.MethodPost, "/v1/transfers/bank/lookup", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ─── HTTP core ────────────────────────────────────────────────────────────────

// envelope is the standard Nomba response wrapper: {code, description, data}.
type envelope struct {
	Code        string          `json:"code"`
	Description string          `json:"description"`
	Message     string          `json:"message"`
	Status      *bool           `json:"status"`
	Data        json.RawMessage `json:"data"`
}

// doJSON performs an authenticated request and unmarshals the `data` field of the
// standard envelope into out (if non-nil).
func (c *Client) doJSON(ctx context.Context, method, path string, body, out interface{}) error {
	token, err := c.token(ctx)
	if err != nil {
		return err
	}

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("accountId", c.accountID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nomba %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return parseAPIError(resp.StatusCode, raw)
	}
	if out == nil {
		return nil
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("nomba %s %s: decode envelope: %w", method, path, err)
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("nomba %s %s: decode data: %w", method, path, err)
	}
	return nil
}

func parseAPIError(status int, raw []byte) *APIError {
	apiErr := &APIError{StatusCode: status, rawBody: string(raw)}
	_ = json.Unmarshal(raw, apiErr)
	return apiErr
}
