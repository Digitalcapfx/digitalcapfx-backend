package nilos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultBaseURL = "https://api.nilos.io/v1"

// Client is the Nilos fiat banking API client.
// Nilos provides virtual IBAN/accounts, multi-currency balances, and SWIFT/SEPA/CEMAC transfers.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type Option func(*Client)

func WithBaseURL(url string) Option {
	return func(c *Client) { c.baseURL = url }
}

func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL: defaultBaseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, o := range opts {
		o(c)
	}
	return c
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
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nilos POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("nilos POST %s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) doGet(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nilos GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("nilos GET %s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
