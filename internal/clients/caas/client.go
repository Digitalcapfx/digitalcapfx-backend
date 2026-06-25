// Package caas is the DigitalFX client for the Rach CaaS Settlement Gateway.
//
// CaaS is a stablecoin settlement routing engine for mobile money networks.
// It provisions ERC-4337 Smart Contract Wallets (SCW) per phone number and
// executes gasless peer-to-peer stablecoin transfers on-chain.
//
// Architecture:
//   - Each DigitalFX end-user gets one SCW identified by their E.164 phone number.
//   - DigitalFX (the MMO tenant) holds a USDC treasury balance on CaaS.
//   - Fiat deposits flow: HUB2 mobile money → FundUser call → credits user's SCW.
//   - P2P transfers: sender_phone → recipient_phone, stablecoin moves on-chain.
//
// Authentication: X-API-Key header with rach_sk_live_... or rach_sk_test_... key.
// Store the key in CAAS_API_KEY.
package caas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const defaultBaseURL = "https://rach-caas-api-dx75yvdhaq-nw.a.run.app"

// Client is the Rach CaaS API client.
type Client struct {
	baseURL    string
	apiKey     string // rach_sk_live_... or rach_sk_test_... (X-API-Key header)
	httpClient *http.Client
}

// Option configures the Client.
type Option func(*Client)

// WithBaseURL overrides the default CaaS base URL.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = u }
}

// WithTimeout overrides the default 30-second HTTP timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.httpClient.Timeout = d }
}

// New creates a CaaS client authenticated with the given API key.
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
	return fmt.Sprintf("caas api %d: %s", e.Status, e.Message)
}

// ── Token — USDT or USDC ─────────────────────────────────────────────────────

type Token string

const (
	TokenUSDC Token = "USDC"
	TokenUSDT Token = "USDT"
)

// ── ProvisionSCW ─────────────────────────────────────────────────────────────

// ProvisionSCWRequest registers a new end-user by phone number.
// POST /v1/users/provision
type ProvisionSCWRequest struct {
	// PhoneNumber must be in E.164 format e.g. "+22507000000".
	PhoneNumber string `json:"phone_number"`
}

// ProvisionSCWResponse is returned after successfully provisioning an SCW.
// The wallet_address is deterministic — calling provision again with the same
// phone returns the same address without creating a new wallet.
type ProvisionSCWResponse struct {
	// BlindIndex is the HMAC of the phone number, used as the user's privacy-
	// preserving identifier in all subsequent dashboard and compliance calls.
	BlindIndex    string `json:"blind_index"`
	WalletAddress string `json:"wallet_address"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
}

// ── GetBalance ───────────────────────────────────────────────────────────────

// BalanceResponse is returned by GetBalance.
// GET /v1/users/balance?phone_number=...
type BalanceResponse struct {
	// BalanceUSDC is the live USDC balance on the user's SCW (decimal string).
	BalanceUSDC   string `json:"balance_usdc"`
	WalletAddress string `json:"wallet_address"`
}

// ── FundUser ─────────────────────────────────────────────────────────────────

// FundUserRequest credits a user's SCW after a confirmed fiat deposit.
// DigitalFX calls this after HUB2 confirms a Mobile Money collection.
// POST /v1/users/fund
type FundUserRequest struct {
	// DepositID is the idempotency key — typically the HUB2 payment reference.
	// Replaying the same DepositID returns 409 without double-crediting.
	DepositID string `json:"deposit_id"`
	// PhoneNumber of the depositing user (E.164).
	PhoneNumber string `json:"phone_number"`
	// LocalFiatAmount is the fiat amount received e.g. "5000" (XOF).
	LocalFiatAmount string `json:"local_fiat_amount"`
	// StablecoinAmount is the USDC/USDT equivalent (decimal string).
	// Derived from the FX rate at time of deposit. See GetFXQuote.
	StablecoinAmount string `json:"stablecoin_amount"`
	// TargetToken is USDC or USDT.
	TargetToken Token `json:"target_token"`
}

// FundUserResponse is returned after a successful fund instruction is queued.
type FundUserResponse struct {
	DepositID string `json:"deposit_id"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

// ── Transfer (P2P stablecoin) ─────────────────────────────────────────────────

// TransferRequest sends stablecoin from one user to another by phone number.
// CaaS handles the ERC-4337 bundler, gas sponsorship (paymaster), and on-chain
// settlement — DigitalFX sees only a transfer_id and async status updates.
// POST /v1/transfers/send
type TransferRequest struct {
	// IdempotencyKey prevents duplicate submissions. Use a UUID or reference.
	IdempotencyKey string `json:"idempotency_key"`
	// SenderPhone is the E.164 phone number of the sender.
	SenderPhone string `json:"sender_phone"`
	// RecipientPhone is the E.164 phone number of the recipient.
	RecipientPhone string `json:"recipient_phone"`
	// LocalFiatAmount is the fiat-denominated value of the transfer (decimal string e.g. "5500.00").
	// For a stablecoin-only transfer, obtain this by calling GetFXQuote first.
	// For direct USDC transfers, pass the USDC amount here and use a DIRECT_CRYPTO quote_id.
	LocalFiatAmount string `json:"local_fiat_amount"`
	// QuoteID locks in the exchange rate from a prior GetFXQuote call.
	QuoteID string `json:"quote_id"`
	// TargetToken is USDC or USDT.
	TargetToken Token `json:"target_token"`
}

// TransferResponse is returned immediately after the transfer is anchored.
// Settlement happens asynchronously — poll or use webhooks for finality.
type TransferResponse struct {
	TransferID string `json:"transfer_id"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	CreatedAt  string `json:"created_at"`
}

// ── GetFXQuote ───────────────────────────────────────────────────────────────

// FXQuoteRequest returns the stablecoin equivalent of a fiat amount.
// Use this before FundUser or Transfer to get the quote_id and rate.
// POST /v1/fx/quote
type FXQuoteRequest struct {
	// FiatAmount is the local currency amount e.g. 5000.
	FiatAmount float64 `json:"fiat_amount"`
	// LocalCurrency is the ISO 4217 code e.g. "XOF", "NGN", "USD".
	LocalCurrency string `json:"local_currency"`
	// TargetToken is "USDT" or "USDC". Defaults to USDT if omitted.
	TargetToken Token `json:"target_token,omitempty"`
}

// FXQuoteResponse contains the locked rate and quote_id needed for transfers.
type FXQuoteResponse struct {
	QuoteID       string `json:"quote_id"`
	CurrencyPair  string `json:"currency_pair"`   // e.g. "XOF_USDC"
	FiatAmount    string `json:"fiat_amount"`      // decimal string e.g. "20000.00"
	LocalCurrency string `json:"local_currency"`
	// ExpectedOut is the stablecoin amount the customer will receive (decimal string).
	// Renamed from stablecoin_amount in CaaS API v1.0.1.
	ExpectedOut string `json:"expected_out"`
	Rate        string `json:"rate"`
	ExpiresAt   string `json:"expires_at"`
	TargetToken Token  `json:"target_token"`
}

// ── GetTenantBalance ─────────────────────────────────────────────────────────

// TenantBalanceResponse is returned by GetTenantBalance.
// This is DigitalFX's own USDC treasury balance inside CaaS —
// it must be positive to fund user SCWs.
// GET /v1/dashboard/treasury/tenant-balance
type TenantBalanceResponse struct {
	MmoID          string `json:"mmo_id"`
	AvailableUSDC  string `json:"available_usdc"`
	ReservedUSDC   string `json:"reserved_usdc"`
	TotalFundedUSDC string `json:"total_funded_usdc"`
}

// ── UserAccountResponse ───────────────────────────────────────────────────────

// UserAccountResponse represents a provisioned end-user.
type UserAccountResponse struct {
	UserIndex     string `json:"user_index"` // blind_index
	WalletAddress string `json:"wallet_address"`
	AccountStatus string `json:"account_status"`
	DateProvisioned string `json:"date_provisioned"`
}

// ── API methods ──────────────────────────────────────────────────────────────

// ProvisionSCW registers a new end-user and returns their deterministic ERC-4337
// smart contract wallet address. Safe to call multiple times — idempotent.
// POST /v1/users/provision
func (c *Client) ProvisionSCW(ctx context.Context, req ProvisionSCWRequest) (*ProvisionSCWResponse, error) {
	var out ProvisionSCWResponse
	if err := c.post(ctx, "/v1/users/provision", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetBalance retrieves the live USDC balance on a user's SCW via eth_call.
// GET /v1/users/balance?phone_number=<E.164>
func (c *Client) GetBalance(ctx context.Context, phoneNumber string) (*BalanceResponse, error) {
	q := url.Values{}
	q.Set("phone_number", phoneNumber)
	var out BalanceResponse
	if err := c.get(ctx, "/v1/users/balance", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FundUser credits a user's SCW with stablecoin after a confirmed fiat deposit.
// Call this after HUB2 sends a SUCCESSFUL webhook for a Mobile Money collection.
// The deposit_id is the idempotency key — replaying returns 409.
// POST /v1/users/fund
func (c *Client) FundUser(ctx context.Context, req FundUserRequest) (*FundUserResponse, error) {
	var out FundUserResponse
	if err := c.post(ctx, "/v1/users/fund", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Transfer executes a gasless peer-to-peer stablecoin transfer identified by
// phone number. CaaS resolves wallet addresses, sponsors gas via the paymaster,
// and settles on-chain asynchronously. Use webhooks or poll GetTransfer for finality.
// POST /v1/transfers/send
func (c *Client) Transfer(ctx context.Context, req TransferRequest) (*TransferResponse, error) {
	var out TransferResponse
	if err := c.post(ctx, "/v1/transfers/send", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetFXQuote calculates the stablecoin equivalent of a fiat amount and returns
// a locked quote_id valid for a short window. Pass this quote_id to FundUser
// or Transfer to lock in the rate at time of quote.
// POST /v1/fx/quote
func (c *Client) GetFXQuote(ctx context.Context, req FXQuoteRequest) (*FXQuoteResponse, error) {
	var out FXQuoteResponse
	if err := c.post(ctx, "/v1/fx/quote", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Withdraw (off-ramp) ───────────────────────────────────────────────────────

// WithdrawRequest initiates an off-ramp: converts stablecoin from user's SCW to
// Mobile Money. CaaS debits the SCW and disburses fiat to the payout_mobile number.
// POST /v1/users/withdraw
type WithdrawRequest struct {
	// Phone is the E.164 phone number of the user whose SCW to debit.
	Phone string `json:"phone"`
	// Amount is the stablecoin amount to withdraw (decimal string e.g. "50.00").
	Amount string `json:"amount"`
	// Token is USDC or USDT.
	Token Token `json:"token"`
	// PayoutMobile is the Mobile Money number to disburse fiat to (E.164).
	PayoutMobile string `json:"payout_mobile"`
	// PayoutNetwork is the MNO operator e.g. "Orange", "MTN", "Wave".
	PayoutNetwork string `json:"payout_network"`
	// IdempotencyKey prevents duplicate withdrawals. Use a stable UUID.
	IdempotencyKey string `json:"idempotency_key"`
}

// WithdrawResponse is returned after a withdrawal is queued by CaaS.
type WithdrawResponse struct {
	WithdrawalID string `json:"withdrawal_id"`
	Status       string `json:"status"`
	Message      string `json:"message"`
}

// Withdraw initiates a stablecoin → Mobile Money off-ramp for an end-user.
// CaaS debits the user's SCW and dispatches the fiat disbursement asynchronously.
// POST /v1/users/withdraw
func (c *Client) Withdraw(ctx context.Context, req WithdrawRequest) (*WithdrawResponse, error) {
	var out WithdrawResponse
	if err := c.post(ctx, "/v1/users/withdraw", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── UpdatePhone ───────────────────────────────────────────────────────────────

// UpdatePhoneRequest re-links a user's SCW to a new phone number.
// CaaS recomputes the blind_index from new_phone_number and returns it.
// POST /v1/users/update-phone
type UpdatePhoneRequest struct {
	OldPhoneNumber string `json:"old_phone_number"`
	NewPhoneNumber string `json:"new_phone_number"`
}

// UpdatePhoneResponse is returned after the phone number is updated.
// CaaS v1.0.1 returns the same shape as ProvisionSCWResponse.
type UpdatePhoneResponse struct {
	// BlindIndex is the new HMAC identifier for the user after the phone change.
	BlindIndex    string `json:"blind_index"`
	WalletAddress string `json:"wallet_address"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
}

// UpdatePhone re-links an end-user's SCW to a new phone number.
// Call this whenever a DigitalFX user changes their registered phone.
// The returned BlindIndex must replace the locally cached value in caas_wallets.
// POST /v1/users/update-phone
func (c *Client) UpdatePhone(ctx context.Context, req UpdatePhoneRequest) (*UpdatePhoneResponse, error) {
	var out UpdatePhoneResponse
	if err := c.post(ctx, "/v1/users/update-phone", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Treasury Topup ────────────────────────────────────────────────────────────

// TreasuryTopupRequest notifies CaaS that DigitalFX has transferred fiat to
// the Rach CaaS settlement bank account. The topup is initially PENDING.
// POST /v1/dashboard/treasury/topup
type TreasuryTopupRequest struct {
	// Amount is the fiat amount sent to the CaaS bank (decimal string e.g. "500000.00").
	Amount string `json:"amount"`
	// Reference is a unique identifier for this transfer (e.g. bank reference number).
	Reference string `json:"reference"`
	// Token is the target stablecoin to receive: USDC or USDT.
	Token Token `json:"token"`
	// Note is optional context (e.g. "EOD settlement batch 2026-06-21").
	Note string `json:"note,omitempty"`
}

// TreasuryTopupResponse is returned after submitting a topup notification.
type TreasuryTopupResponse struct {
	ID        string `json:"id"`
	Status    string `json:"status"` // PENDING until Rach confirms fiat receipt
	Amount    float64 `json:"amount"`
	Reference string `json:"reference"`
	CreatedAt string `json:"created_at"`
}

// SubmitTreasuryTopup notifies Rach CaaS that DigitalFX has sent fiat to their
// bank account in Ivory Coast. This is the programmatic representation of step 6
// in the funding flow. Rach CaaS will confirm receipt and credit the tenant's
// USDC treasury, which then funds individual user SCWs via FundUser.
// POST /v1/dashboard/treasury/topup
func (c *Client) SubmitTreasuryTopup(ctx context.Context, req TreasuryTopupRequest) (*TreasuryTopupResponse, error) {
	var out TreasuryTopupResponse
	if err := c.post(ctx, "/v1/dashboard/treasury/topup", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ConfirmTreasuryTopup marks a pending topup as confirmed and immediately credits
// the tenant's USDC balance. Called by Rach CaaS ops after verifying fiat receipt.
// POST /v1/dashboard/treasury/topup/{id}/confirm
func (c *Client) ConfirmTreasuryTopup(ctx context.Context, topupID, creditedBy string) error {
	body := map[string]string{"credited_by": creditedBy}
	var out map[string]any
	return c.post(ctx, fmt.Sprintf("/v1/dashboard/treasury/topup/%s/confirm", topupID), body, &out)
}

// GetTenantBalance returns DigitalFX's own internal USDC treasury balance.
// This is the balance available to fund user SCWs — must be topped up via
// the Rach dashboard when low.
// GET /v1/dashboard/treasury/tenant-balance
func (c *Client) GetTenantBalance(ctx context.Context) (*TenantBalanceResponse, error) {
	var out TenantBalanceResponse
	if err := c.get(ctx, "/v1/dashboard/treasury/tenant-balance", nil, &out); err != nil {
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
		return fmt.Errorf("caas: build request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("caas: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("caas: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("caas: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errBody struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		msg := errBody.Error
		if msg == "" {
			msg = errBody.Message
		}
		if msg == "" {
			msg = resp.Status
		}
		return &APIError{Status: resp.StatusCode, Message: msg}
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("caas: decode response: %w", err)
		}
	}
	return nil
}
