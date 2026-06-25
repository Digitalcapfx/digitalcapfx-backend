package handlers

import "time"

// ─── Common ───────────────────────────────────────────────────────────────────

// ErrorResponse is the standard error envelope returned on 4xx/5xx.
type ErrorResponse struct {
	Success bool `json:"success" example:"false"`
	Error   struct {
		Code    string `json:"code" example:"VALIDATION_ERROR"`
		Message string `json:"message" example:"phone is required"`
	} `json:"error"`
}

// MessageResponse is returned when there is no data payload, only a confirmation.
type MessageResponse struct {
	Success bool   `json:"success" example:"true"`
	Message string `json:"message" example:"OTP sent"`
}

// ─── Auth ─────────────────────────────────────────────────────────────────────

type SendOTPRequest struct {
	Phone string `json:"phone" example:"+237612345678"`
}

type VerifyOTPRequest struct {
	Phone string `json:"phone" example:"+237612345678"`
	Code  string `json:"code" example:"123456"`
}

type RegisterRequest struct {
	Phone     string `json:"phone" example:"+237612345678"`
	Email     string `json:"email" example:"alice@example.com"`
	FirstName string `json:"first_name" example:"Alice"`
	LastName  string `json:"last_name" example:"Dupont"`
	PIN       string `json:"pin" example:"123456"`
}

type LoginRequest struct {
	Phone string `json:"phone" example:"+237612345678"`
	PIN   string `json:"pin" example:"123456"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" example:"eyJhbGci..."`
}

type TokenPairData struct {
	AccessToken  string `json:"access_token" example:"eyJhbGci..."`
	RefreshToken string `json:"refresh_token" example:"eyJhbGci..."`
	ExpiresIn    int64  `json:"expires_in" example:"1800"`
	SessionID    string `json:"session_id" example:"550e8400-e29b-41d4-a716-446655440000"`
}

type TokenPairResponse struct {
	Success bool          `json:"success" example:"true"`
	Data    TokenPairData `json:"data"`
}

// ─── Accounts ─────────────────────────────────────────────────────────────────

type AccountData struct {
	ID               string    `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	UserID           string    `json:"user_id" example:"550e8400-e29b-41d4-a716-446655440001"`
	Currency         string    `json:"currency" example:"XAF"`
	Balance          string    `json:"balance" example:"5000.000000"`
	AvailableBalance string    `json:"available_balance" example:"5000.000000"`
	AccountNumber    string    `json:"account_number" example:"DFX0000001"`
	Status           string    `json:"status" example:"active"`
	CreatedAt        time.Time `json:"created_at"`
}

type AccountListResponse struct {
	Success bool          `json:"success" example:"true"`
	Data    []AccountData `json:"data"`
}

type AccountResponse struct {
	Success bool        `json:"success" example:"true"`
	Data    AccountData `json:"data"`
}

type TransactionData struct {
	ID          string    `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Reference   string    `json:"reference" example:"TXN-20240101-001"`
	AccountID   string    `json:"account_id"`
	Type        string    `json:"type" example:"credit"`
	Amount      string    `json:"amount" example:"5000.000000"`
	Currency    string    `json:"currency" example:"XAF"`
	Fee         string    `json:"fee" example:"0.000000"`
	Description string    `json:"description" example:"Deposit via MTN Mobile Money"`
	Status      string    `json:"status" example:"completed"`
	CreatedAt   time.Time `json:"created_at"`
}

type PaginationMeta struct {
	Page       int `json:"page" example:"1"`
	PerPage    int `json:"per_page" example:"20"`
	Total      int `json:"total" example:"100"`
	TotalPages int `json:"total_pages" example:"5"`
}

type TransactionListResponse struct {
	Success bool              `json:"success" example:"true"`
	Data    []TransactionData `json:"data"`
	Meta    PaginationMeta    `json:"meta"`
}

type TransactionResponse struct {
	Success bool            `json:"success" example:"true"`
	Data    TransactionData `json:"data"`
}

// ─── WaaS Wallets ─────────────────────────────────────────────────────────────

type CreateWalletRequest struct {
	// Network is the blockchain network. Supported: BTC BCH LTC BSC ETH POL TRX SOL XRP
	Network string `json:"network" example:"ETH" enums:"BTC,BCH,LTC,BSC,ETH,POL,TRX,SOL,XRP"`
}

type WalletData struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	WaasWalletID string    `json:"waas_wallet_id" example:"cust_abc123"`
	Network      string    `json:"network" example:"ETH"`
	Address      string    `json:"address" example:"0xabc123..."`
	IsDefault    bool      `json:"is_default" example:"true"`
	CreatedAt    time.Time `json:"created_at"`
}

type WalletListResponse struct {
	Success bool         `json:"success" example:"true"`
	Data    []WalletData `json:"data"`
}

type WalletResponse struct {
	Success bool       `json:"success" example:"true"`
	Data    WalletData `json:"data"`
}

type DepositRequest struct {
	Currency string  `json:"currency" example:"XAF"`
	Amount   float64 `json:"amount" example:"5000"`
	Phone    string  `json:"phone" example:"+237612345678"`
	Operator string  `json:"operator" example:"MTN"`
}

type WithdrawalRequest struct {
	Currency string  `json:"currency" example:"XAF"`
	Amount   float64 `json:"amount" example:"5000"`
	Phone    string  `json:"phone" example:"+237612345678"`
	Operator string  `json:"operator" example:"ORANGE"`
}

type Hub2RefData struct {
	Hub2Reference string `json:"hub2_reference" example:"HUB2-REF-001"`
}

type Hub2RefResponse struct {
	Success bool        `json:"success" example:"true"`
	Data    Hub2RefData `json:"data"`
}

// ─── CaaS / Crypto ────────────────────────────────────────────────────────────

type CaasWalletData struct {
	ID                 string    `json:"id"`
	UserID             string    `json:"user_id"`
	CaasWalletID       string    `json:"caas_wallet_id" example:"0xblindindex..."`
	AbstractionAddress string    `json:"abstraction_address" example:"0xSCW123..."`
	IsActive           bool      `json:"is_active" example:"true"`
	CreatedAt          time.Time `json:"created_at"`
}

type CaasWalletResponse struct {
	Success bool           `json:"success" example:"true"`
	Data    CaasWalletData `json:"data"`
}

type BalanceData struct {
	BalanceUSDC   string `json:"balance_usdc" example:"100.000000"`
	WalletAddress string `json:"wallet_address" example:"0xSCW123..."`
}

type CryptoBalanceResponse struct {
	Success bool        `json:"success" example:"true"`
	Data    BalanceData `json:"data"`
}

// FundAccountRequest is the body for POST /crypto/fund.
// The customer specifies how much XOF/XAF to collect via Mobile Money
// and which stablecoin they want in their Instant USD Account.
type FundAccountRequest struct {
	// Currency of the Mobile Money deposit: XOF or XAF
	Currency string  `json:"currency" example:"XOF" enums:"XOF,XAF"`
	// Amount in local fiat (minimum 100)
	Amount   float64 `json:"amount" example:"20000" minimum:"100"`
	// Phone is the Mobile Money number to pull from (E.164)
	Phone    string  `json:"phone" example:"+22507000000"`
	// Operator is the Mobile Money provider
	Operator string  `json:"operator" example:"MTN" enums:"Orange,MTN,Wave,Moov,Airtel"`
	// Token is the target stablecoin — defaults to USDC
	Token    string  `json:"token" example:"USDC" enums:"USDC,USDT"`
}

type SendCryptoRequest struct {
	ReceiverPhone string `json:"receiver_phone" example:"+237698765432"`
	// Token must be USDT or USDC
	Token  string `json:"token" example:"USDC" enums:"USDT,USDC"`
	Amount string `json:"amount" example:"50.00"`
}

type CryptoTxData struct {
	ID            string    `json:"id"`
	Reference     string    `json:"reference" example:"CRYPTO-REF-001"`
	SenderUserID  string    `json:"sender_user_id"`
	ReceiverPhone string    `json:"receiver_phone" example:"+237698765432"`
	Token         string    `json:"token" example:"USDC"`
	Amount        string    `json:"amount" example:"50.00"`
	TxHash        string    `json:"tx_hash,omitempty"`
	Status        string    `json:"status" example:"pending"`
	CreatedAt     time.Time `json:"created_at"`
}

type CryptoTxResponse struct {
	Success bool         `json:"success" example:"true"`
	Data    CryptoTxData `json:"data"`
}

type CryptoTxListResponse struct {
	Success bool           `json:"success" example:"true"`
	Data    []CryptoTxData `json:"data"`
}

// ─── Transfers ────────────────────────────────────────────────────────────────

type InternalTransferRequest struct {
	ReceiverPhone string  `json:"receiver_phone" example:"+237698765432"`
	Currency      string  `json:"currency" example:"XAF"`
	Amount        float64 `json:"amount" example:"5000"`
	Description   string  `json:"description" example:"Monthly payment"`
}

type Hub2PaymentRequest struct {
	Currency  string  `json:"currency" example:"XAF"`
	Amount    float64 `json:"amount" example:"5000"`
	Phone     string  `json:"phone" example:"+237612345678"`
	Operator  string  `json:"operator" example:"MTN"`
	// PaymentMethod e.g. mobile_money
	PaymentMethod string `json:"payment_method" example:"mobile_money"`
	// Direction: collection (deposit) or disbursement (withdrawal)
	Direction string `json:"direction" example:"collection" enums:"collection,disbursement"`
}

// ─── Admin ────────────────────────────────────────────────────────────────────

type AdminKYCRejectRequest struct {
	Reason string `json:"reason" example:"Document image is blurry or expired"`
}

// ─── Auth Extended ────────────────────────────────────────────────────────────

type GoogleSignInRequest struct {
	IDToken string `json:"id_token" example:"eyJhbGci..."`
}

type GoogleSignInResponse struct {
	Success   bool          `json:"success" example:"true"`
	IsNewUser bool          `json:"is_new_user" example:"true"`
	Data      TokenPairData `json:"data"`
}

type ForgotPINRequest struct {
	EmailOrPhone string `json:"email_or_phone" example:"alice@example.com"`
}

type ResetPINRequest struct {
	EmailOrPhone string `json:"email_or_phone" example:"alice@example.com"`
	Code         string `json:"code" example:"123456"`
	NewPIN       string `json:"new_pin" example:"654321"`
}

type VerifyEmailRequest struct {
	Code string `json:"code" example:"123456"`
}

type DeviceData struct {
	ID         string    `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	DeviceName string    `json:"device_name" example:"iPhone"`
	DeviceIP   string    `json:"device_ip" example:"41.202.207.10"`
	LastUsedAt time.Time `json:"last_used_at"`
	CreatedAt  time.Time `json:"created_at"`
	IsCurrent  bool      `json:"is_current" example:"true"`
}

type DeviceListResponse struct {
	Success bool         `json:"success" example:"true"`
	Data    []DeviceData `json:"data"`
}

// ─── Profile ──────────────────────────────────────────────────────────────────

type ProfileData struct {
	ID              string  `json:"id"`
	PhoneNumber     string  `json:"phone_number" example:"+237612345678"`
	Email           *string `json:"email,omitempty" example:"alice@example.com"`
	FirstName       string  `json:"first_name" example:"Alice"`
	LastName        string  `json:"last_name" example:"Dupont"`
	Bio             *string `json:"bio,omitempty" example:"Digital finance enthusiast from Cameroon"`
	AvatarURL       *string `json:"avatar_url,omitempty"`
	DateOfBirth     *string `json:"date_of_birth,omitempty" example:"1995-06-15"`
	Nationality     *string `json:"nationality,omitempty" example:"Cameroonian"`
	KycStatus       string  `json:"kyc_status" example:"pending"`
	IsEmailVerified bool    `json:"is_email_verified" example:"false"`
}

type ProfileResponse struct {
	Success bool        `json:"success" example:"true"`
	Data    ProfileData `json:"data"`
}

type UpdateProfileRequest struct {
	FirstName   string  `json:"first_name" example:"Alice"`
	LastName    string  `json:"last_name" example:"Dupont"`
	Bio         *string `json:"bio,omitempty" example:"Digital finance enthusiast"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
	DateOfBirth *string `json:"date_of_birth,omitempty" example:"1995-06-15"`
	Nationality *string `json:"nationality,omitempty" example:"Cameroonian"`
}

// ─── MetaMap KYC ──────────────────────────────────────────────────────────────

type MetaMapInitData struct {
	ApplicantID    string `json:"applicant_id" example:"60f1a2b3c4d5e6f7a8b9c0d1"`
	IdentityAccess string `json:"identity_access" example:"eyJhbGci..."`
	FlowID         string `json:"flow_id" example:"60f1a2b3c4d5e6f7a8b9c0d2"`
	Status         string `json:"status" example:"pending"`
}

type MetaMapInitResponse struct {
	Success bool            `json:"success" example:"true"`
	Data    MetaMapInitData `json:"data"`
}

// ─── Webhooks ─────────────────────────────────────────────────────────────────

// HUB2WebhookRequest mirrors hub2.WebhookPayload for Swagger documentation.
type HUB2WebhookRequest struct {
	Reference string  `json:"reference" example:"HUB2-REF-001"`
	Status    string  `json:"status" example:"SUCCESSFUL" enums:"SUCCESSFUL,FAILED,CANCELLED,PENDING"`
	Amount    float64 `json:"amount" example:"20000"`
	Currency  string  `json:"currency" example:"XOF"`
	Phone     string  `json:"phone" example:"+22507000000"`
	Type      string  `json:"type" example:"COLLECTION" enums:"COLLECTION,DISBURSEMENT"`
}

// ─── KYC ─────────────────────────────────────────────────────────────────────

type KYCStatusData struct {
	KycStatus string `json:"kyc_status" example:"pending" enums:"pending,approved,rejected"`
}

type KYCStatusResponse struct {
	Success bool          `json:"success" example:"true"`
	Data    KYCStatusData `json:"data"`
}

type KYCDocumentRequest struct {
	// DocType one of: national_id | passport | selfie | proof_of_address
	DocType string `json:"doc_type" example:"national_id" enums:"national_id,passport,selfie,proof_of_address"`
	// DocURL is the GCS object path or signed URL returned by the upload endpoint
	DocURL string `json:"doc_url" example:"https://storage.googleapis.com/digitalfx-kyc-dev/doc.jpg"`
}

type KYCDocumentData struct {
	ID              string    `json:"id"`
	UserID          string    `json:"user_id"`
	DocType         string    `json:"doc_type" example:"national_id"`
	DocURL          string    `json:"doc_url"`
	Status          string    `json:"status" example:"pending"`
	RejectionReason string    `json:"rejection_reason,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type KYCDocumentResponse struct {
	Success bool            `json:"success" example:"true"`
	Data    KYCDocumentData `json:"data"`
}

type KYCDocumentListResponse struct {
	Success bool              `json:"success" example:"true"`
	Data    []KYCDocumentData `json:"data"`
}
