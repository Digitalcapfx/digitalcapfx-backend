package nilos

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
	"time"
)

// Sandbox: https://app-demo.nilos.io  |  Production: https://app.nilos.io
const defaultBaseURL = "https://app-demo.nilos.io"

// Client is the Nilos fiat banking API client.
// Auth: HMAC-SHA256 — every request carries X-Api-Key (key ID) and
// X-Api-Signature = hex(HMAC-SHA256(urlPath + requestBody, apiSecret)).
type Client struct {
	baseURL    string
	apiKey     string // key ID → X-Api-Key header
	apiSecret  string // signing secret → HMAC-SHA256
	httpClient *http.Client
}

type Option func(*Client)

func WithBaseURL(url string) Option {
	return func(c *Client) { c.baseURL = url }
}

// New creates a Nilos client. apiKey is the key ID; apiSecret is the HMAC signing secret.
func New(apiKey, apiSecret string, opts ...Option) *Client {
	c := &Client{
		baseURL:   defaultBaseURL,
		apiKey:    apiKey,
		apiSecret: apiSecret,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// sign returns hex(HMAC-SHA256(path+body, apiSecret)).
func (c *Client) sign(path, body string) string {
	mac := hmac.New(sha256.New, []byte(c.apiSecret))
	mac.Write([]byte(path + body))
	return hex.EncodeToString(mac.Sum(nil))
}

// ─── Customer ─────────────────────────────────────────────────────────────────

type CreateCustomerRequest struct {
	ExternalID string `json:"external_id"` // our user UUID
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	Email      string `json:"email"`
	Phone      string `json:"phone"`
	Country    string `json:"country"` // ISO 3166-1 alpha-2, e.g. "CM"
}

type CustomerResponse struct {
	ID         string    `json:"id"`
	ExternalID string    `json:"external_id"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

func (c *Client) CreateCustomer(ctx context.Context, req CreateCustomerRequest) (*CustomerResponse, error) {
	var out CustomerResponse
	return &out, c.doPost(ctx, "/customers", req, &out)
}

// ─── Account ──────────────────────────────────────────────────────────────────

type CreateAccountRequest struct {
	CustomerID  string `json:"customer_id"`
	Currency    string `json:"currency"` // ISO 4217: USD, EUR, GBP, XAF, XOF
	AccountName string `json:"account_name"`
	Reference   string `json:"reference"` // our account UUID
}

type AccountResponse struct {
	ID          string    `json:"id"`
	CustomerID  string    `json:"customer_id"`
	Currency    string    `json:"currency"`
	AccountName string    `json:"account_name"`
	IBAN        string    `json:"iban"`
	BIC         string    `json:"bic"`
	Balance     string    `json:"balance"`   // decimal string, e.g. "12450.75"
	Available   string    `json:"available"` // after holds
	Status      string    `json:"status"`    // "active" | "suspended"
	CreatedAt   time.Time `json:"created_at"`
}

func (c *Client) CreateAccount(ctx context.Context, req CreateAccountRequest) (*AccountResponse, error) {
	var out AccountResponse
	return &out, c.doPost(ctx, "/accounts", req, &out)
}

func (c *Client) GetAccount(ctx context.Context, nilosAccountID string) (*AccountResponse, error) {
	var out AccountResponse
	return &out, c.doGet(ctx, "/accounts/"+nilosAccountID, &out)
}

// ─── Transactions ─────────────────────────────────────────────────────────────

type NilosTransaction struct {
	ID          string    `json:"id"`
	AccountID   string    `json:"account_id"`
	Type        string    `json:"type"`        // "credit" | "debit"
	Amount      string    `json:"amount"`      // decimal string
	Currency    string    `json:"currency"`
	Description string    `json:"description"`
	Reference   string    `json:"reference"`
	CounterName string    `json:"counter_name"` // sender/recipient name
	Status      string    `json:"status"`       // "pending" | "completed" | "failed"
	CreatedAt   time.Time `json:"created_at"`
}

type ListTransactionsResponse struct {
	Data  []NilosTransaction `json:"data"`
	Total int                `json:"total"`
	Page  int                `json:"page"`
}

func (c *Client) ListTransactions(ctx context.Context, nilosAccountID string, page, limit int) (*ListTransactionsResponse, error) {
	var out ListTransactionsResponse
	path := fmt.Sprintf("/accounts/%s/transactions?page=%d&limit=%d", nilosAccountID, page, limit)
	return &out, c.doGet(ctx, path, &out)
}

// ─── Transfer ─────────────────────────────────────────────────────────────────

type TransferRequest struct {
	FromAccountID string `json:"from_account_id"`
	ToAccountID   string `json:"to_account_id,omitempty"` // internal DigitalFX → DigitalFX
	ToBIC         string `json:"to_bic,omitempty"`        // external SWIFT
	ToIBAN        string `json:"to_iban,omitempty"`       // external SEPA
	BenefName     string `json:"beneficiary_name,omitempty"`
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
	Description   string `json:"description"`
	Reference     string `json:"reference"` // idempotency key
}

type TransferResponse struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"` // "pending" | "completed" | "failed"
	Reference string    `json:"reference"`
	CreatedAt time.Time `json:"created_at"`
}

func (c *Client) Transfer(ctx context.Context, req TransferRequest) (*TransferResponse, error) {
	var out TransferResponse
	return &out, c.doPost(ctx, "/transfers", req, &out)
}

// ─── FX Quote ─────────────────────────────────────────────────────────────────

type FXQuoteRequest struct {
	FromCurrency string `json:"from_currency"`
	ToCurrency   string `json:"to_currency"`
	Amount       string `json:"amount"`
}

type FXQuoteResponse struct {
	QuoteID     string    `json:"quote_id"`
	Rate        string    `json:"rate"`
	FromAmount  string    `json:"from_amount"`
	ToAmount    string    `json:"to_amount"`
	Fee         string    `json:"fee"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func (c *Client) GetFXQuote(ctx context.Context, req FXQuoteRequest) (*FXQuoteResponse, error) {
	var out FXQuoteResponse
	return &out, c.doPost(ctx, "/fx/quote", req, &out)
}

// ─── HTTP helpers ─────────────────────────────────────────────────────────────

func (c *Client) doPost(ctx context.Context, path string, body, out interface{}) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("X-Api-Signature", c.sign(path, string(b)))

	return c.do(req, path, out)
}

func (c *Client) doGet(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("X-Api-Signature", c.sign(path, ""))

	return c.do(req, path, out)
}

func (c *Client) do(req *http.Request, path string, out interface{}) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nilos %s %s: %w", req.Method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("nilos %s %s: status %d: %s", req.Method, path, resp.StatusCode, string(errBody))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
