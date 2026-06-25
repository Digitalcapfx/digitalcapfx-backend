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
	"net/url"
	"strconv"
	"time"
)

// Sandbox: https://app-demo.nilos.io  |  Production: https://app.nilos.io
const defaultBaseURL = "https://app-demo.nilos.io"

// Payment rail constants for DigitalFX's supported corridors.
const (
	RailSEPA          = "SEPA"            // EUR — European IBAN
	RailSWIFT         = "SWIFT"           // USD/GBP — international SWIFT
	RailFPS           = "FPS"             // GBP — UK Faster Payments
	RailMobileMoneyCM = "MOBILE_MONEY_CM" // XAF — Cameroon (MTN, Orange)
	RailMobileMoneySN = "MOBILE_MONEY_SN" // XOF — Senegal
	RailMobileMoneyCi = "MOBILE_MONEY_CI" // XOF — Ivory Coast
	RailCEMACBank     = "CEMAC_BANK"      // XAF — CEMAC bank-to-bank
	RailUEMOA         = "UEMOA"           // XOF — UEMOA bank-to-bank
)

// Payout side: SELL = amount expressed in source; BUY = amount expressed in target.
const (
	SideSell = "SELL"
	SideBuy  = "BUY"
)

// Client is the Nilos fiat banking API client.
//
// Every request is signed with HMAC-SHA256:
//
//	X-Api-Key:       <apiKey ID>
//	X-Api-Signature: hex(HMAC-SHA256(path + "?" + queryString + body, apiSecret))
//
// Query string is included in the signature even for GET requests.
// For requests without a query string the "?" separator is omitted.
type Client struct {
	baseURL    string
	apiKey     string // key ID
	apiSecret  string // HMAC-SHA256 signing secret
	orgID      string // optional x-nilos-org header for multi-org
	httpClient *http.Client
}

type Option func(*Client)

func WithBaseURL(u string) Option        { return func(c *Client) { c.baseURL = u } }
func WithOrgID(id string) Option         { return func(c *Client) { c.orgID = id } }
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.httpClient.Timeout = d } }

// New creates a Nilos client.
// apiKey is the key ID (X-Api-Key); apiSecret is the HMAC signing secret (X-Api-Signature).
func New(apiKey, apiSecret string, opts ...Option) *Client {
	c := &Client{
		baseURL:    defaultBaseURL,
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// sign returns hex(HMAC-SHA256(path + "?" + queryString + body, apiSecret)).
// queryString must NOT include the leading "?".
func (c *Client) sign(path, queryString, body string) string {
	input := path
	if queryString != "" {
		input += "?" + queryString
	}
	input += body
	mac := hmac.New(sha256.New, []byte(c.apiSecret))
	mac.Write([]byte(input))
	return hex.EncodeToString(mac.Sum(nil))
}

// ─── Shared response types ────────────────────────────────────────────────────

type PageMeta struct {
	Total int `json:"total"`
	Page  int `json:"page"`
	Limit int `json:"limit"`
}

type APIError struct {
	StatusCode int    `json:"-"`
	Message    string `json:"message"`
	rawBody    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("nilos %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("nilos %d: %s", e.StatusCode, e.rawBody)
}

// ─── Account ─────────────────────────────────────────────────────────────────
//
// Nilos accounts live at the organisation level — not per user.
// DigitalFX creates one account per currency per user and stores the nilos ID
// in the accounts.nilos_account_id column.

type AccountBalance struct {
	Amount      float64  `json:"amount"`
	Currency    string   `json:"currency"`
	RefCurrency string   `json:"refCurrency"` // reference currency (usually EUR)
	RefAmount   *float64 `json:"refAmount"`
}

// Account is a Nilos virtual account on a payment rail.
// Details is rail-specific:
//   - SEPA/CEMAC_BANK/UAEFTS → { iban, bic, ... }
//   - SPEI                   → { clabe, ... }
//   - Crypto                 → { address, ... }
//   - Mobile Money           → { phoneNumber, provider, ... }
type Account struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Rail      string                 `json:"rail"`
	Details   map[string]interface{} `json:"details"`
	Balance   []AccountBalance       `json:"balance"`
	CreatedAt time.Time              `json:"createdAt"`
	UpdatedAt time.Time              `json:"updatedAt"`
}

// BalanceFor returns the balance for the given ISO currency code, or 0 if absent.
func (a *Account) BalanceFor(currency string) float64 {
	for _, b := range a.Balance {
		if b.Currency == currency {
			return b.Amount
		}
	}
	return 0
}

// DetailString extracts a string field from the rail-specific Details map.
func (a *Account) DetailString(key string) string {
	if v, ok := a.Details[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

type CreateAccountRequest struct {
	Name string `json:"name"` // display name, e.g. "Rach Finance EUR Account"
	Rail string `json:"rail"` // e.g. "SEPA", "SWIFT", "MOBILE_MONEY_CM"
}

type AccountListResponse struct {
	Data []Account `json:"data"`
	Meta PageMeta  `json:"meta"`
}

type AccountSearchParams struct {
	Page   int
	Limit  int
	SortBy string // e.g. "updatedAt:DESC"
	Name   string // $ilike filter on account name
}

// CreateAccount provisions a new virtual account on the given rail.
// POST /v1/account
func (c *Client) CreateAccount(ctx context.Context, req CreateAccountRequest) (*Account, error) {
	var out Account
	return &out, c.doPost(ctx, "/v1/account", req, &out)
}

// GetAccount returns a single account by Nilos UUID, including balances and rail details.
// GET /v1/account/:id
func (c *Client) GetAccount(ctx context.Context, id string) (*Account, error) {
	var out Account
	return &out, c.doGet(ctx, "/v1/account/"+id, nil, &out)
}

// ListAccounts returns all active accounts for the organisation (capped at 1000).
// GET /v1/accounts
func (c *Client) ListAccounts(ctx context.Context) ([]Account, error) {
	var out []Account
	return out, c.doGet(ctx, "/v1/accounts", nil, &out)
}

// SearchAccounts returns a paginated, filterable list of accounts.
// GET /v1/accounts/search
func (c *Client) SearchAccounts(ctx context.Context, p AccountSearchParams) (*AccountListResponse, error) {
	q := url.Values{}
	if p.Page > 0 {
		q.Set("page", strconv.Itoa(p.Page))
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.SortBy != "" {
		q.Set("sortBy", p.SortBy)
	}
	if p.Name != "" {
		q.Set("filter.name", "$ilike:"+p.Name)
	}
	var out AccountListResponse
	return &out, c.doGet(ctx, "/v1/accounts/search", q, &out)
}

// DeleteAccount archives an account by ID.
// DELETE /v1/account/:id
func (c *Client) DeleteAccount(ctx context.Context, id string) error {
	return c.doDelete(ctx, "/v1/account/"+id)
}

// ─── Transactions ─────────────────────────────────────────────────────────────

// Transaction is an inbound or outbound movement on a Nilos account.
type Transaction struct {
	ID         string    `json:"id"`
	Reference  string    `json:"reference"`
	ExternalID string    `json:"externalId"`
	IsInbound  bool      `json:"isInbound"`
	Currency   string    `json:"currency"`
	Amount     float64   `json:"amount"`
	AccountID  string    `json:"accountId"`
	Status     string    `json:"status"` // "success" | "pending" | "failed"
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type TransactionListResponse struct {
	Data []Transaction `json:"data"`
	Meta PageMeta      `json:"meta"`
}

type TransactionSearchParams struct {
	Page      int
	Limit     int
	SortBy    string // e.g. "timestamp:DESC"
	AccountID string // filter.accountId — filter to a specific account
	Currency  string // filter.currency
	IsInbound *bool  // filter.isInbound
	Status    string // filter.status: "success" | "pending" | "failed"
	Reference string // filter.reference ($ilike)
	ExternalID string // filter.externalId ($eq)
}

// ListAllTransactions returns all transactions for the organisation (capped at 1000).
// GET /v1/transactions
func (c *Client) ListAllTransactions(ctx context.Context) ([]Transaction, error) {
	var out []Transaction
	return out, c.doGet(ctx, "/v1/transactions", nil, &out)
}

// SearchTransactions returns a paginated, filterable transaction list.
// GET /v1/transactions/search
func (c *Client) SearchTransactions(ctx context.Context, p TransactionSearchParams) (*TransactionListResponse, error) {
	q := url.Values{}
	if p.Page > 0 {
		q.Set("page", strconv.Itoa(p.Page))
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.SortBy != "" {
		q.Set("sortBy", p.SortBy)
	}
	if p.AccountID != "" {
		q.Set("filter.accountId", "$eq:"+p.AccountID)
	}
	if p.Currency != "" {
		q.Set("filter.currency", "$eq:"+p.Currency)
	}
	if p.IsInbound != nil {
		if *p.IsInbound {
			q.Set("filter.isInbound", "$eq:true")
		} else {
			q.Set("filter.isInbound", "$eq:false")
		}
	}
	if p.Status != "" {
		q.Set("filter.status", p.Status)
	}
	if p.Reference != "" {
		q.Set("filter.reference", "$ilike:"+p.Reference)
	}
	if p.ExternalID != "" {
		q.Set("filter.externalId", "$eq:"+p.ExternalID)
	}
	var out TransactionListResponse
	return &out, c.doGet(ctx, "/v1/transactions/search", q, &out)
}

// GetTransaction returns a single transaction by ID.
// GET /v1/transaction/:id
func (c *Client) GetTransaction(ctx context.Context, id string) (*Transaction, error) {
	var out Transaction
	return &out, c.doGet(ctx, "/v1/transaction/"+id, nil, &out)
}

// ─── Recipients ───────────────────────────────────────────────────────────────
//
// Recipients are external payout destinations (bank accounts, mobile money
// numbers, crypto addresses). Each recipient is tied to a payment rail.

// RecipientInfo holds the rail-specific fields for a payout recipient.
// Only populate the fields required by the target rail.
type RecipientInfo struct {
	// Common across most rails
	RecipientName    string `json:"recipientName,omitempty"`
	RecipientAddress string `json:"recipientAddress,omitempty"`
	RecipientCountry string `json:"recipientCountry,omitempty"` // ISO 3166-1 alpha-2
	AccountType      string `json:"accountType,omitempty"`      // "personal" | "business"

	// SEPA / CEMAC_BANK / UAEFTS
	IBAN string `json:"iban,omitempty"`

	// SWIFT
	SwiftCode               string             `json:"swiftCode,omitempty"`
	AccountNumber           string             `json:"accountNumber,omitempty"`
	InstitutionNumber       string             `json:"institutionNumber,omitempty"`
	TransitNumber           string             `json:"transitNumber,omitempty"`
	FundsDestinationCountry string             `json:"fundsDestinationCountry,omitempty"`
	RecipientStructuredAddr *StructuredAddress `json:"recipientStructuredAddress,omitempty"`

	// FPS (UK Faster Payments)
	SortCode string `json:"sortCode,omitempty"`

	// SPEI (Mexico)
	CLABE string `json:"clabe,omitempty"`

	// NIP (Nigeria)
	NIPCode string `json:"nipCode,omitempty"`

	// PIX (Brazil)
	PIX string `json:"pix,omitempty"`

	// Mobile Money (MOBILE_MONEY_CI/SN/TG/BJ/CM)
	PhoneNumber string `json:"phoneNumber,omitempty"`
	Provider    string `json:"provider,omitempty"` // "Orange" | "MTN" | "Moov" | "Wave" | ...

	// UEMOA
	BankCode string `json:"bankCode,omitempty"`

	// Crypto (ETH / MATIC / BNB / TRX / SOL)
	Address   string `json:"address,omitempty"`
	LegalName string `json:"legalName,omitempty"`
}

type StructuredAddress struct {
	Street     string `json:"street,omitempty"`
	City       string `json:"city,omitempty"`
	State      string `json:"state,omitempty"`
	PostalCode string `json:"postalCode,omitempty"`
	Country    string `json:"country,omitempty"`
	ISOCode    string `json:"isoCode,omitempty"`
}

type CreateRecipientRequest struct {
	Name          string        `json:"name"`
	Rail          string        `json:"rail"`
	RecipientInfo RecipientInfo `json:"recipientInfo"`
}

type Recipient struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Rail      string    `json:"rail"`
	Type      string    `json:"type"` // "bank" | "crypto"
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type RecipientListResponse struct {
	Data []Recipient `json:"data"`
	Meta PageMeta    `json:"meta"`
}

type RecipientSearchParams struct {
	Page   int
	Limit  int
	SortBy string
	Name   string // $ilike
	Type   string // "bank" | "crypto"
	Rail   string // e.g. "SEPA"
}

// CreateRecipient creates a payout destination on the given rail.
// POST /v1/recipient
func (c *Client) CreateRecipient(ctx context.Context, req CreateRecipientRequest) (*Recipient, error) {
	var out Recipient
	return &out, c.doPost(ctx, "/v1/recipient", req, &out)
}

// GetRecipient returns a single recipient by ID.
// GET /v1/recipient/:id
func (c *Client) GetRecipient(ctx context.Context, id string) (*Recipient, error) {
	var out Recipient
	return &out, c.doGet(ctx, "/v1/recipient/"+id, nil, &out)
}

// ListAllRecipients returns all recipients for the organisation (capped at 1000).
// GET /v1/recipients
func (c *Client) ListAllRecipients(ctx context.Context) ([]Recipient, error) {
	var out []Recipient
	return out, c.doGet(ctx, "/v1/recipients", nil, &out)
}

// SearchRecipients returns a paginated, filterable recipient list.
// GET /v1/recipients/search
func (c *Client) SearchRecipients(ctx context.Context, p RecipientSearchParams) (*RecipientListResponse, error) {
	q := url.Values{}
	if p.Page > 0 {
		q.Set("page", strconv.Itoa(p.Page))
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.SortBy != "" {
		q.Set("sortBy", p.SortBy)
	}
	if p.Name != "" {
		q.Set("filter.name", "$ilike:"+p.Name)
	}
	if p.Type != "" {
		q.Set("filter.type", "$eq:"+p.Type)
	}
	if p.Rail != "" {
		q.Set("filter.rail", "$eq:"+p.Rail)
	}
	var out RecipientListResponse
	return &out, c.doGet(ctx, "/v1/recipients/search", q, &out)
}

// DeleteRecipient removes a recipient by ID.
// DELETE /v1/recipient/:id
func (c *Client) DeleteRecipient(ctx context.Context, id string) error {
	return c.doDelete(ctx, "/v1/recipient/"+id)
}

// UpdateRecipientName renames a recipient.
// PATCH /v1/recipient/:id
func (c *Client) UpdateRecipientName(ctx context.Context, id, name string) (*Recipient, error) {
	var out Recipient
	return &out, c.doPatch(ctx, "/v1/recipient/"+id, map[string]string{"name": name}, &out)
}

// UpdateCryptoRecipient updates the legal name and account type for a crypto recipient.
// PATCH /v1/recipient/crypto/:id
func (c *Client) UpdateCryptoRecipient(ctx context.Context, id, legalName, accountType string) (*Recipient, error) {
	var out Recipient
	return &out, c.doPatch(ctx, "/v1/recipient/crypto/"+id, map[string]string{
		"legalName":   legalName,
		"accountType": accountType,
	}, &out)
}

// ─── Quote ────────────────────────────────────────────────────────────────────
//
// A quote locks an FX rate for cross-currency payouts.
// Pass the quote ID to CreatePayoutTransfer when the currencies differ.

type CreateQuoteRequest struct {
	SourceCurrency string  `json:"sourceCurrency"`
	SourceRail     string  `json:"sourceRail"`
	TargetCurrency string  `json:"targetCurrency"`
	TargetRail     string  `json:"targetRail"`
	Amount         float64 `json:"amount"`
	Side           string  `json:"side"` // SideSell | SideBuy
}

type Quote struct {
	ID             string    `json:"id"`
	SourceCurrency string    `json:"sourceCurrency"`
	SourceRail     string    `json:"sourceRail"`
	TargetCurrency string    `json:"targetCurrency"`
	TargetRail     string    `json:"targetRail"`
	Rate           float64   `json:"rate"`
	SourceAmount   float64   `json:"sourceAmount"`
	TargetAmount   float64   `json:"targetAmount"`
	Side           string    `json:"side"`
	ExpiresAt      time.Time `json:"expiresAt"`
	CreatedAt      time.Time `json:"createdAt"`
}

// CreateQuote creates an FX exchange quote between two rails/currencies.
// Pass the returned ID to CreatePayoutTransfer to lock the rate.
// POST /v1/quote
func (c *Client) CreateQuote(ctx context.Context, req CreateQuoteRequest) (*Quote, error) {
	var out Quote
	return &out, c.doPost(ctx, "/v1/quote", req, &out)
}

// GetQuote retrieves a previously created quote.
// GET /v1/quote/:id
func (c *Client) GetQuote(ctx context.Context, id string) (*Quote, error) {
	var out Quote
	return &out, c.doGet(ctx, "/v1/quote/"+id, nil, &out)
}

// ─── Payout ───────────────────────────────────────────────────────────────────
//
// Payouts are outbound movements:
//   - Transfer: funds from a Nilos account to an external recipient
//   - Swap:     FX conversion within a single account (no recipient needed)

type CreatePayoutTransferRequest struct {
	AccountID      string  `json:"accountId"`
	SourceCurrency string  `json:"sourceCurrency"`
	RecipientID    string  `json:"recipientId"`
	TargetCurrency string  `json:"targetCurrency"`
	Amount         float64 `json:"amount"`
	Side           string  `json:"side"`              // SideSell | SideBuy
	Reference      string  `json:"reference"`         // idempotency key / invoice ref
	Reason         string  `json:"reason,omitempty"`  // "GOODS_SERVICES" | "SALARY" | "FAMILY_SUPPORT" | ...
	QuoteID        string  `json:"quoteId,omitempty"` // required when currencies differ
}

type CreatePayoutSwapRequest struct {
	AccountID      string  `json:"accountId"`
	Amount         float64 `json:"amount"`
	SourceCurrency string  `json:"sourceCurrency"`
	TargetCurrency string  `json:"targetCurrency"`
	Side           string  `json:"side"` // SideSell | SideBuy
}

type Payout struct {
	ID             string    `json:"id"`
	AccountID      string    `json:"accountId"`
	RecipientID    string    `json:"recipientId"`
	Currency       string    `json:"currency"`       // source currency
	TargetCurrency string    `json:"targetCurrency"`
	Amount         float64   `json:"amount"`
	TargetAmount   float64   `json:"targetAmount"`
	Status         string    `json:"status"` // "pending" | "completed" | "failed"
	Reference      string    `json:"reference"`
	TransactionID  string    `json:"transactionId"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type PayoutListResponse struct {
	Data []Payout `json:"data"`
	Meta PageMeta `json:"meta"`
}

type PayoutSearchParams struct {
	Page           int
	Limit          int
	SortBy         string
	AccountID      string
	RecipientID    string
	TargetCurrency string
	Status         string
}

// CreatePayoutTransfer sends funds from a Nilos account to an external recipient.
// Same-currency transfers omit QuoteID; cross-currency transfers require a QuoteID.
// POST /v1/payout/transfer
func (c *Client) CreatePayoutTransfer(ctx context.Context, req CreatePayoutTransferRequest) (*Payout, error) {
	var out Payout
	return &out, c.doPost(ctx, "/v1/payout/transfer", req, &out)
}

// CreatePayoutSwap converts currency inside a single account (no recipient required).
// POST /v1/payout/swap
func (c *Client) CreatePayoutSwap(ctx context.Context, req CreatePayoutSwapRequest) (*Payout, error) {
	var out Payout
	return &out, c.doPost(ctx, "/v1/payout/swap", req, &out)
}

// GetPayout returns one payout by ID, including status and source/target amounts.
// GET /v1/payout/:id
func (c *Client) GetPayout(ctx context.Context, id string) (*Payout, error) {
	var out Payout
	return &out, c.doGet(ctx, "/v1/payout/"+id, nil, &out)
}

// ListAllPayouts returns all payouts for the organisation (capped at 1000).
// GET /v1/payouts
func (c *Client) ListAllPayouts(ctx context.Context) ([]Payout, error) {
	var out []Payout
	return out, c.doGet(ctx, "/v1/payouts", nil, &out)
}

// SearchPayouts returns a paginated, filterable payout list.
// GET /v1/payouts/search
func (c *Client) SearchPayouts(ctx context.Context, p PayoutSearchParams) (*PayoutListResponse, error) {
	q := url.Values{}
	if p.Page > 0 {
		q.Set("page", strconv.Itoa(p.Page))
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.SortBy != "" {
		q.Set("sortBy", p.SortBy)
	}
	if p.AccountID != "" {
		q.Set("filter.accountId", "$eq:"+p.AccountID)
	}
	if p.RecipientID != "" {
		q.Set("filter.recipientId", "$eq:"+p.RecipientID)
	}
	if p.TargetCurrency != "" {
		q.Set("filter.targetCurrency", "$eq:"+p.TargetCurrency)
	}
	if p.Status != "" {
		q.Set("filter.status", "$eq:"+p.Status)
	}
	var out PayoutListResponse
	return &out, c.doGet(ctx, "/v1/payouts/search", q, &out)
}

// ─── Misc ─────────────────────────────────────────────────────────────────────

type MobileMoneyProvider struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type BankInfo struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// GetMobileMoneyProviders returns supported mobile money providers for a rail.
// rail: e.g. "mobile_money_cm" for Cameroon.
// GET /v1/misc/mobile-money-providers/:rail
func (c *Client) GetMobileMoneyProviders(ctx context.Context, rail string) ([]MobileMoneyProvider, error) {
	var out []MobileMoneyProvider
	return out, c.doGet(ctx, "/v1/misc/mobile-money-providers/"+rail, nil, &out)
}

// GetBanks returns supported banks for a rail (e.g. "NIP" for Nigeria).
// GET /v1/misc/banks/:rail
func (c *Client) GetBanks(ctx context.Context, rail string) ([]BankInfo, error) {
	var out []BankInfo
	return out, c.doGet(ctx, "/v1/misc/banks/"+rail, nil, &out)
}

// ─── Demo sandbox only ────────────────────────────────────────────────────────

type SimulateDepositRequest struct {
	AccountID string  `json:"accountId"`
	Currency  string  `json:"currency"`
	Amount    float64 `json:"amount"`
	Reference string  `json:"reference"`
}

// SimulateDeposit triggers a fake inbound deposit in the sandbox environment.
// POST /v1/deposit
func (c *Client) SimulateDeposit(ctx context.Context, req SimulateDepositRequest) error {
	return c.doPost(ctx, "/v1/deposit", req, nil)
}

// ─── HTTP helpers ─────────────────────────────────────────────────────────────

func (c *Client) doPost(ctx context.Context, path string, body, out interface{}) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	bodyStr := string(b)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req, path, "", bodyStr)
	return c.do(req, out)
}

func (c *Client) doPatch(ctx context.Context, path string, body, out interface{}) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req, path, "", string(b))
	return c.do(req, out)
}

func (c *Client) doGet(ctx context.Context, path string, q url.Values, out interface{}) error {
	queryString := q.Encode()
	fullURL := c.baseURL + path
	if queryString != "" {
		fullURL += "?" + queryString
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return err
	}
	c.setAuth(req, path, queryString, "")
	return c.do(req, out)
}

func (c *Client) doDelete(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	c.setAuth(req, path, "", "")
	return c.do(req, nil)
}

func (c *Client) setAuth(req *http.Request, path, queryString, body string) {
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("X-Api-Signature", c.sign(path, queryString, body))
	if c.orgID != "" {
		req.Header.Set("x-nilos-org", c.orgID)
	}
}

func (c *Client) do(req *http.Request, out interface{}) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nilos %s %s: %w", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		apiErr := &APIError{StatusCode: resp.StatusCode, rawBody: string(rawBody)}
		_ = json.Unmarshal(rawBody, apiErr) // try to extract message field
		return apiErr
	}

	if out == nil {
		return nil
	}
	return json.Unmarshal(rawBody, out)
}
