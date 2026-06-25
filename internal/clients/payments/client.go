// Package payments is the DigitalFX client for the Rach Finance Payments API.
// It covers the Wallet-as-a-Service (WaaS) surface: HD wallet creation, address
// derivation, transfers, transaction history, gas estimation, and listings.
//
// Authentication: every request sends the business API key in the X-API-Key header.
// Key prefix determines the environment — test_sk_* = sandbox, live_sk_* = production.
package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const defaultBaseURL = "https://payments-api-dev-966260606560.europe-west2.run.app"

// Client is the Rach Finance WaaS API client.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// Option configures the Client.
type Option func(*Client)

// WithBaseURL overrides the default API base URL (useful for tests / staging).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = u }
}

// WithTimeout overrides the default 30-second HTTP timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.httpClient.Timeout = d }
}

// New creates a Client authenticated with the given API key.
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

// ── Error type ───────────────────────────────────────────────────────────────

// APIError is returned when the server responds with a non-2xx status.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("payments api %d: %s", e.Status, e.Message)
}

// ── Request / Response types ─────────────────────────────────────────────────

// Network enumerates the supported blockchain networks.
type Network string

const (
	NetworkBTC Network = "BTC"
	NetworkBCH Network = "BCH"
	NetworkLTC Network = "LTC"
	NetworkBSC Network = "BSC"
	NetworkETH Network = "ETH"
	NetworkPOL Network = "POL"
	NetworkTRX Network = "TRX"
	NetworkSOL Network = "SOL"
	NetworkXRP Network = "XRP"
)

// ── CreateCustomerWallet ─────────────────────────────────────────────────────

// CreateCustomerWalletRequest is the body for POST /api/v1/wallet/customers.
type CreateCustomerWalletRequest struct {
	// CustomerID is your internal identifier for the end-user (e.g. UUID, user_id).
	CustomerID string `json:"customer_id"`
	// WordCount is the mnemonic length. Accepted values: 12, 24. Defaults to 12.
	WordCount int `json:"word_count,omitempty"`
}

// CreateCustomerWalletResponse is returned on successful wallet creation.
// Mnemonic is ONLY returned once — store it securely; it cannot be re-fetched
// without the wallet:reveal_seed permission.
type CreateCustomerWalletResponse struct {
	CustomerID string   `json:"customer_id"`
	WalletID   uint     `json:"wallet_id"`
	Mnemonic   []string `json:"mnemonic"`
	CreatedAt  string   `json:"created_at"`
}

// ── DeriveAddress ────────────────────────────────────────────────────────────

// DeriveAddressRequest is the body for POST /api/v1/wallet/:customerID/derive.
type DeriveAddressRequest struct {
	// Network is required. One of BTC BCH LTC BSC ETH POL TRX SOL XRP.
	Network Network `json:"network"`
	// Index is the BIP-44 address index. Defaults to 0.
	Index uint32 `json:"index,omitempty"`
	// IsTestnet derives a testnet address when true.
	IsTestnet bool `json:"is_testnet,omitempty"`
	// EnableMonitoring subscribes this address to the payment monitor so
	// incoming deposits trigger webhook events.
	EnableMonitoring bool `json:"enable_monitoring,omitempty"`
}

// DeriveAddressResponse is returned after a successful address derivation.
type DeriveAddressResponse struct {
	CustomerID     string  `json:"customer_id"`
	Network        string  `json:"network"`
	Address        string  `json:"address"`
	Index          uint32  `json:"index"`
	DerivationPath string  `json:"derivation_path"`
	IsTestnet      bool    `json:"is_testnet"`
	Monitored      bool    `json:"monitored"`
}

// ── ExportPrivateKey ─────────────────────────────────────────────────────────

// ExportPrivateKeyRequest is the body for POST /api/v1/wallet/:customerID/export-key.
// Requires the wallet:export_key API key permission.
type ExportPrivateKeyRequest struct {
	Network Network `json:"network"`
	// Index must match the index used when DeriveAddress was called.
	Index uint32 `json:"index,omitempty"`
}

// ExportPrivateKeyResponse carries the plain-text private key.
type ExportPrivateKeyResponse struct {
	Address    string `json:"address"`
	PrivateKey string `json:"private_key"`
	Network    string `json:"network"`
}

// ── GetSeedPhrase ────────────────────────────────────────────────────────────

// GetSeedPhraseResponse is returned by GET /api/v1/wallet/:customerID/seed.
// Requires the wallet:reveal_seed API key permission.
type GetSeedPhraseResponse struct {
	CustomerID string   `json:"customer_id"`
	Mnemonic   []string `json:"mnemonic"`
	WordCount  int      `json:"word_count"`
}

// ── Transfer ─────────────────────────────────────────────────────────────────

// TransferRequest is the body for POST /api/v1/wallet/:customerID/transfer.
// Requires the wallet:transfer API key permission.
type TransferRequest struct {
	// Network is the blockchain to send on.
	Network Network `json:"network"`
	// Currency is the token symbol (ETH, BNB, USDT, USDC, TRX, SOL, BTC, …).
	Currency string `json:"currency"`
	// ToAddress is the destination wallet address.
	ToAddress string `json:"to_address"`
	// Amount is the transfer amount in the smallest on-chain unit as a decimal
	// string: wei for EVM chains, lamports for SOL, satoshis for BTC/LTC/BCH,
	// sun for TRX, drops for XRP.
	Amount string `json:"amount"`
	// Index is the address derivation index to send from. Defaults to 0.
	Index uint32 `json:"index,omitempty"`
}

// TransferResponse is returned after a successful broadcast.
type TransferResponse struct {
	TxHash      string    `json:"tx_hash"`
	FromAddress string    `json:"from_address"`
	ToAddress   string    `json:"to_address"`
	Amount      string    `json:"amount"`
	Currency    string    `json:"currency"`
	Network     string    `json:"network"`
	GasFee      string    `json:"gas_fee"`
	Status      string    `json:"status"`
	Timestamp   time.Time `json:"timestamp"`
}

// ── EstimateGas ──────────────────────────────────────────────────────────────

// EstimateGasRequest is the body for POST /api/v1/wallet/estimate-gas.
// This endpoint is PUBLIC — no API key is required.
// Network must be one of BSC, ETH, POL (EVM chains only).
type EstimateGasRequest struct {
	Network     Network `json:"network"`
	Currency    string  `json:"currency"`
	FromAddress string  `json:"from_address"`
	ToAddress   string  `json:"to_address"`
	// Amount in wei.
	Amount string `json:"amount"`
}

// GasEstimate is returned by EstimateGas.
type GasEstimate struct {
	Network      string `json:"network"`
	Currency     string `json:"currency"`
	EstimatedFee string `json:"estimated_fee"` // raw units (wei / sun / lamports)
	FeeInNative  string `json:"fee_in_native"` // human-readable (ETH / BNB / MATIC)
	GasPrice     string `json:"gas_price"`
	GasLimit     uint64 `json:"gas_limit"`
	FastFee      string `json:"fast_fee"` // 1.5× gas price
	SlowFee      string `json:"slow_fee"` // 0.8× gas price
}

// ── GetTransactions ──────────────────────────────────────────────────────────

// GetTransactionsParams are the query parameters for GET /api/v1/wallet/:customerID/transactions.
type GetTransactionsParams struct {
	Page     int
	Limit    int
	Network  Network
	Currency string
	Status   string // pending | confirmed | failed
}

// GetTransactionsResponse wraps the paginated transaction list.
type GetTransactionsResponse struct {
	CustomerID   string            `json:"customer_id"`
	Transactions []json.RawMessage `json:"transactions"` // raw — shape varies per chain
	Page         int               `json:"page"`
	Limit        int               `json:"limit"`
	Total        int               `json:"total"`
}

// ── ListCustomerAddresses ─────────────────────────────────────────────────────

// ListCustomerAddressesResponse is returned by GET /api/v1/wallet/:customerID/addresses.
type ListCustomerAddressesResponse struct {
	CustomerID string            `json:"customer_id"`
	Addresses  []json.RawMessage `json:"addresses"` // DerivedAddress + Balances preloaded
	Total      int               `json:"total"`
}

// ── ListBusinessAddresses ────────────────────────────────────────────────────

// ListBusinessAddressesParams are the query parameters for GET /api/v1/wallet/addresses.
type ListBusinessAddressesParams struct {
	Page     int
	Limit    int
	Network  Network
	Currency string
	Search   string // address prefix or customer ID substring
}

// BusinessAddress is a single entry from the list-all-addresses endpoint.
type BusinessAddress struct {
	ID             uint              `json:"id"`
	CustomerID     string            `json:"customer_id"`
	Address        string            `json:"address"`
	Network        string            `json:"network"`
	Currency       string            `json:"currency"`
	Index          uint32            `json:"index"`
	DerivationPath string            `json:"derivation_path"`
	Monitored      bool              `json:"monitored"`
	TotalReceived  string            `json:"total_received"`
	CreatedAt      time.Time         `json:"created_at"`
	Balances       []json.RawMessage `json:"balances"`
}

// ListBusinessAddressesResponse wraps the paginated address list.
type ListBusinessAddressesResponse struct {
	Addresses []BusinessAddress `json:"addresses"`
	Total     int               `json:"total"`
	Page      int               `json:"page"`
	Limit     int               `json:"limit"`
}

// ── ListCustomers ─────────────────────────────────────────────────────────────

// ListCustomersParams are the query parameters for GET /api/v1/wallet/customers.
type ListCustomersParams struct {
	Page   int
	Limit  int
	Search string // customer_id substring match
}

// ListCustomersResponse wraps the paginated customer list.
type ListCustomersResponse struct {
	Customers []json.RawMessage `json:"customers"` // CustomerWallet records
	Total     int               `json:"total"`
	Page      int               `json:"page"`
	Limit     int               `json:"limit"`
}

// ── API methods ──────────────────────────────────────────────────────────────

// CreateCustomerWallet provisions a new BIP-44 HD wallet for the given customer.
// The mnemonic is returned exactly once — store it securely.
// POST /api/v1/wallet/customers
func (c *Client) CreateCustomerWallet(ctx context.Context, req CreateCustomerWalletRequest) (*CreateCustomerWalletResponse, error) {
	var out CreateCustomerWalletResponse
	if err := c.post(ctx, "/api/v1/wallet/customers", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListCustomers returns a paginated list of all customers that have wallets
// provisioned under the authenticated business account.
// GET /api/v1/wallet/customers
func (c *Client) ListCustomers(ctx context.Context, p ListCustomersParams) (*ListCustomersResponse, error) {
	q := url.Values{}
	if p.Page > 0 {
		q.Set("page", strconv.Itoa(p.Page))
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Search != "" {
		q.Set("search", p.Search)
	}

	var out ListCustomersResponse
	if err := c.get(ctx, "/api/v1/wallet/customers", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSeedPhrase retrieves the mnemonic for an existing customer wallet.
// Requires the wallet:reveal_seed permission on the API key.
// GET /api/v1/wallet/:customerID/seed
func (c *Client) GetSeedPhrase(ctx context.Context, customerID string) (*GetSeedPhraseResponse, error) {
	var out GetSeedPhraseResponse
	path := fmt.Sprintf("/api/v1/wallet/%s/seed", url.PathEscape(customerID))
	if err := c.get(ctx, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeriveAddress derives a BIP-44 address for a customer on the requested network.
// Optionally enables Tatum V4 deposit monitoring for incoming funds.
// POST /api/v1/wallet/:customerID/derive
func (c *Client) DeriveAddress(ctx context.Context, customerID string, req DeriveAddressRequest) (*DeriveAddressResponse, error) {
	var out DeriveAddressResponse
	path := fmt.Sprintf("/api/v1/wallet/%s/derive", url.PathEscape(customerID))
	if err := c.post(ctx, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ExportPrivateKey exports the private key for a previously derived address.
// Requires the wallet:export_key permission on the API key.
// POST /api/v1/wallet/:customerID/export-key
func (c *Client) ExportPrivateKey(ctx context.Context, customerID string, req ExportPrivateKeyRequest) (*ExportPrivateKeyResponse, error) {
	var out ExportPrivateKeyResponse
	path := fmt.Sprintf("/api/v1/wallet/%s/export-key", url.PathEscape(customerID))
	if err := c.post(ctx, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Transfer broadcasts a crypto transfer from a customer's HD wallet address.
// Amount must be in the smallest on-chain unit (wei, satoshis, lamports, sun, drops).
// Requires the wallet:transfer permission on the API key.
// POST /api/v1/wallet/:customerID/transfer
func (c *Client) Transfer(ctx context.Context, customerID string, req TransferRequest) (*TransferResponse, error) {
	var out TransferResponse
	path := fmt.Sprintf("/api/v1/wallet/%s/transfer", url.PathEscape(customerID))
	if err := c.post(ctx, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetTransactions returns paginated transaction history for a customer wallet.
// Results can be filtered by network, currency, and status.
// GET /api/v1/wallet/:customerID/transactions
func (c *Client) GetTransactions(ctx context.Context, customerID string, p GetTransactionsParams) (*GetTransactionsResponse, error) {
	q := url.Values{}
	if p.Page > 0 {
		q.Set("page", strconv.Itoa(p.Page))
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Network != "" {
		q.Set("network", string(p.Network))
	}
	if p.Currency != "" {
		q.Set("currency", p.Currency)
	}
	if p.Status != "" {
		q.Set("status", p.Status)
	}

	var out GetTransactionsResponse
	path := fmt.Sprintf("/api/v1/wallet/%s/transactions", url.PathEscape(customerID))
	if err := c.get(ctx, path, q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListCustomerAddresses returns all derived addresses for a customer, with
// their on-chain balances preloaded. Pass refresh=true to trigger a live
// on-chain balance refresh before returning (slower but always accurate).
// GET /api/v1/wallet/:customerID/addresses
func (c *Client) ListCustomerAddresses(ctx context.Context, customerID string, refresh bool) (*ListCustomerAddressesResponse, error) {
	q := url.Values{}
	if refresh {
		q.Set("refresh", "true")
	}

	var out ListCustomerAddressesResponse
	path := fmt.Sprintf("/api/v1/wallet/%s/addresses", url.PathEscape(customerID))
	if err := c.get(ctx, path, q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListBusinessAddresses returns all derived addresses across every customer
// belonging to the authenticated business, with optional filters.
// GET /api/v1/wallet/addresses
func (c *Client) ListBusinessAddresses(ctx context.Context, p ListBusinessAddressesParams) (*ListBusinessAddressesResponse, error) {
	q := url.Values{}
	if p.Page > 0 {
		q.Set("page", strconv.Itoa(p.Page))
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Network != "" {
		q.Set("network", string(p.Network))
	}
	if p.Currency != "" {
		q.Set("currency", p.Currency)
	}
	if p.Search != "" {
		q.Set("search", p.Search)
	}

	var out ListBusinessAddressesResponse
	if err := c.get(ctx, "/api/v1/wallet/addresses", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// EstimateGas estimates on-chain transaction fees for EVM networks (ETH, BSC, POL).
// This endpoint is PUBLIC — no API key is required; the client sends none.
// POST /api/v1/wallet/estimate-gas
func (c *Client) EstimateGas(ctx context.Context, req EstimateGasRequest) (*GasEstimate, error) {
	var out GasEstimate
	if err := c.postPublic(ctx, "/api/v1/wallet/estimate-gas", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── HTTP helpers ─────────────────────────────────────────────────────────────

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("payments: build request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	return c.doPost(ctx, path, body, out, c.apiKey)
}

// postPublic is used for unauthenticated endpoints (estimate-gas).
func (c *Client) postPublic(ctx context.Context, path string, body, out any) error {
	return c.doPost(ctx, path, body, out, "")
}

func (c *Client) doPost(ctx context.Context, path string, body, out any, apiKey string) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("payments: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("payments: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("payments: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		msg := errBody.Error
		if msg == "" {
			msg = resp.Status
		}
		return &APIError{Status: resp.StatusCode, Message: msg}
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("payments: decode response: %w", err)
		}
	}
	return nil
}
