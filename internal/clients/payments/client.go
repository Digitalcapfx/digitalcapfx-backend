package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client wraps the Rach Payments API for Wallet-as-a-Service operations.
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
	UserID  string `json:"user_id"`
	Network string `json:"network"` // BSC | ETH | TRON | SOLANA | BTC | XRP
}

type CreateWalletResponse struct {
	WalletID string `json:"wallet_id"`
	Address  string `json:"address"`
	Network  string `json:"network"`
}

type GetBalanceResponse struct {
	WalletID string  `json:"wallet_id"`
	Network  string  `json:"network"`
	Address  string  `json:"address"`
	Balance  float64 `json:"balance"`
	Token    string  `json:"token"`
}

type TransferRequest struct {
	FromWalletID string  `json:"from_wallet_id"`
	ToAddress    string  `json:"to_address"`
	Amount       float64 `json:"amount"`
	Token        string  `json:"token"`
	Network      string  `json:"network"`
}

type TransferResponse struct {
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

func (c *Client) GetBalance(ctx context.Context, walletID string) (*GetBalanceResponse, error) {
	var resp GetBalanceResponse
	if err := c.get(ctx, fmt.Sprintf("/api/v1/wallets/%s/balance", walletID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Transfer(ctx context.Context, req TransferRequest) (*TransferResponse, error) {
	var resp TransferResponse
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
		return fmt.Errorf("payments api %d: %v", resp.StatusCode, errBody)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
