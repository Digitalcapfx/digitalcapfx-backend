package caas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client wraps the Rach CaaS API for ERC-4337 abstraction wallets and P2P crypto sends.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ── Request / Response types ─────────────────────────────────────────────────

type CreateWalletRequest struct {
	UserID string `json:"user_id"`
	Phone  string `json:"phone"` // used as the P2P identifier
}

type CreateWalletResponse struct {
	WalletID           string `json:"wallet_id"`
	AbstractionAddress string `json:"abstraction_address"`
}

type ResolveByPhoneResponse struct {
	Phone              string `json:"phone"`
	AbstractionAddress string `json:"abstraction_address"`
}

type Balances struct {
	USDT string `json:"usdt"`
	USDC string `json:"usdc"`
}

type SendRequest struct {
	FromAddress string `json:"from_address"`
	ToAddress   string `json:"to_address"`
	Token       string `json:"token"`  // USDT | USDC
	Amount      string `json:"amount"` // string to preserve precision
}

type SendResponse struct {
	TxHash    string `json:"tx_hash"`
	Reference string `json:"reference"`
	Status    string `json:"status"`
}

// ── Methods ──────────────────────────────────────────────────────────────────

func (c *Client) CreateWallet(ctx context.Context, req CreateWalletRequest) (*CreateWalletResponse, error) {
	var resp CreateWalletResponse
	if err := c.post(ctx, "/api/v1/wallets", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ResolveByPhone looks up a DigitalFX user's abstraction address by phone number.
// This is the core of the P2P flow — senders only need the receiver's phone number.
func (c *Client) ResolveByPhone(ctx context.Context, phone string) (*ResolveByPhoneResponse, error) {
	var resp ResolveByPhoneResponse
	if err := c.get(ctx, fmt.Sprintf("/api/v1/wallets/resolve?phone=%s", phone), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetBalances(ctx context.Context, abstractionAddress string) (*Balances, error) {
	var resp Balances
	if err := c.get(ctx, fmt.Sprintf("/api/v1/wallets/%s/balances", abstractionAddress), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Send executes a gasless USDT/USDC transfer between abstraction wallets.
// CaaS handles the bundler, paymaster (gas sponsorship), and on-chain submission.
func (c *Client) Send(ctx context.Context, req SendRequest) (*SendResponse, error) {
	var resp SendResponse
	if err := c.post(ctx, "/api/v1/transfers", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
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
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	return c.do(req, out)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
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
		return fmt.Errorf("caas api %d: %v", resp.StatusCode, errBody)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
