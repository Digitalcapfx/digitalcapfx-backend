// Hand-written query stubs for features whose SQL has not been written yet.
// Add the real query to internal/db/queries/*.sql and run `make sqlc`, then
// delete the corresponding stub here.
package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ── Param structs ─────────────────────────────────────────────────────────────

type UpdateUserRoleParams struct {
	ID   uuid.UUID
	Role string
}

type RecordKycAdminActionParams struct {
	UserID  uuid.UUID
	AdminID uuid.UUID
	Action  string
	Reason  *string
}

type CreateCaasWalletParams struct {
	UserID             uuid.UUID
	CaasWalletID       string
	AbstractionAddress string
}

// ── CaaS new types ────────────────────────────────────────────────────────────

// ── OTP ───────────────────────────────────────────────────────────────────────

// ── Users ─────────────────────────────────────────────────────────────────────

// ── Accounts ──────────────────────────────────────────────────────────────────

// ── Transactions ──────────────────────────────────────────────────────────────

// ── WaaS Wallets ──────────────────────────────────────────────────────────────

// ── CaaS Wallets ──────────────────────────────────────────────────────────────

func (q *Queries) CreateCaasWallet(ctx context.Context, arg CreateCaasWalletParams) (CaasWallet, error) {
	return CaasWallet{}, errNotImplemented
}

// ── Crypto Transactions ───────────────────────────────────────────────────────

// ── Hub2 Payments ─────────────────────────────────────────────────────────────

// ── KYC Documents ─────────────────────────────────────────────────────────────

// ── CaaS Wallets (extended) ───────────────────────────────────────────────────

// ── FX Quotes ─────────────────────────────────────────────────────────────────

// ── CaaS Withdrawals ──────────────────────────────────────────────────────────

// ── Crypto Transaction CaaS extras ────────────────────────────────────────────

// ── Hub2 CaaS funding ─────────────────────────────────────────────────────────

// ── Sessions ──────────────────────────────────────────────────────────────────

// ── Email OTPs ────────────────────────────────────────────────────────────────

// ── MetaMap ───────────────────────────────────────────────────────────────────

// ── User profile ──────────────────────────────────────────────────────────────

// ── Social Auth ───────────────────────────────────────────────────────────────

// ── Admin / KYC management ────────────────────────────────────────────────────

func (q *Queries) ListUsersAwaitingKYCReview(ctx context.Context) ([]UserFull, error) {
	return nil, errNotImplemented
}

func (q *Queries) UpdateUserRole(ctx context.Context, arg UpdateUserRoleParams) (User, error) {
	return User{}, errNotImplemented
}

func (q *Queries) RecordKycAdminAction(ctx context.Context, arg RecordKycAdminActionParams) (KycAdminAction, error) {
	return KycAdminAction{}, errNotImplemented
}

// ── Nilos / Card (migration 000005) ───────────────────────────────────────────

type UpdateAccountNilosParams struct {
	ID              uuid.UUID
	NilosAccountID  string
	NilosCustomerID string
	IBAN            *string
	BIC             *string
}

type CreateVirtualCardParams struct {
	UserID      uuid.UUID
	CardName    string
	LastFour    string
	Currency    string
	CardNetwork string
}

type GetFXRateParams struct {
	BaseCurrency  string
	QuoteCurrency string
}

type UpsertFXRateParams struct {
	BaseCurrency  string
	QuoteCurrency string
	Rate          string
	Source        string
}

func (q *Queries) UpdateAccountNilos(ctx context.Context, arg UpdateAccountNilosParams) error {
	return errNotImplemented
}

func (q *Queries) GetAccountsByUserIDWithNilos(ctx context.Context, userID uuid.UUID) ([]AccountWithNilos, error) {
	return nil, errNotImplemented
}

func (q *Queries) CreateVirtualCard(ctx context.Context, arg CreateVirtualCardParams) (VirtualCard, error) {
	return VirtualCard{}, errNotImplemented
}

func (q *Queries) GetActiveVirtualCard(ctx context.Context, userID uuid.UUID) (VirtualCard, error) {
	return VirtualCard{}, errNotImplemented
}

func (q *Queries) GetFXRate(ctx context.Context, arg GetFXRateParams) (FXRate, error) {
	return FXRate{}, errNotImplemented
}

func (q *Queries) GetAllFXRates(ctx context.Context) ([]FXRate, error) {
	return nil, errNotImplemented
}

func (q *Queries) UpsertFXRate(ctx context.Context, arg UpsertFXRateParams) error {
	return errNotImplemented
}

// ── Dashboard helpers ──────────────────────────────────────────────────────────

type MonthlyTransactionSummary struct {
	IncomeUSD   float64 `json:"income_usd"`
	SpendingUSD float64 `json:"spending_usd"`
	TxCount     int64   `json:"tx_count"`
}

type RecentContact struct {
	Name          string    `json:"name"`
	PhoneNumber   string    `json:"phone_number"`
	LastContactAt time.Time `json:"last_contact_at"`
}

func (q *Queries) GetMonthlyTransactionSummary(ctx context.Context, userID uuid.UUID) (MonthlyTransactionSummary, error) {
	return MonthlyTransactionSummary{}, errNotImplemented
}

func (q *Queries) GetRecentContacts(ctx context.Context, userID uuid.UUID, limit int) ([]RecentContact, error) {
	return nil, errNotImplemented
}

type ActivityItem struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"`      // "fiat" | "crypto" | "caas"
	Type        string    `json:"type"`        // "credit" | "debit" | "exchange"
	Description string    `json:"description"` // "Received BTC", "Sent USD", etc.
	Asset       string    `json:"asset"`       // "BTC", "USD", "USDC", "ETH"
	Amount      string    `json:"amount"`      // absolute decimal string
	AmountSign  string    `json:"amount_sign"` // "+" | "-"
	Status      string    `json:"status"`      // "completed" | "pending" | "failed"
	CounterName string    `json:"counter_name,omitempty"`
	DaysAgo     int       `json:"days_ago"`
	CreatedAt   time.Time `json:"created_at"`
}

// ─── Insights row aliases ─────────────────────────────────────────────────────
// Legacy names kept as aliases of the sqlc-generated row types so services and
// tests can keep using the original identifiers.

type BalanceTrendRow = GetBalanceTrendRow

type MonthlyFlowRow = GetMonthlyFlowRow

type SpendingByTypeRow = GetSpendingByTypeRow

// ─── Notification queries ──────────────────────────────────────────────────────

func (q *Queries) GetUnreadNotificationCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	return 0, errNotImplemented
}

func (q *Queries) GetNotificationByID(ctx context.Context, id uuid.UUID) (Notification, error) {
	return Notification{}, errNotImplemented
}

// ─── Migration 000007: account balance management ─────────────────────────────

type DeductAvailableBalanceParams struct {
	ID     uuid.UUID
	Amount string // decimal string e.g. "100.00"
}

type RestoreAvailableBalanceParams struct {
	ID     uuid.UUID
	Amount string
}

type DeductBalanceParams struct {
	ID     uuid.UUID
	Amount string
}

// DeductAvailableBalance holds funds for a pending withdrawal (reduces available, not settled).
func (q *Queries) DeductAvailableBalance(ctx context.Context, arg DeductAvailableBalanceParams) error {
	return errNotImplemented
}

// RestoreAvailableBalance releases funds held for a failed or cancelled withdrawal.
func (q *Queries) RestoreAvailableBalance(ctx context.Context, arg RestoreAvailableBalanceParams) error {
	return errNotImplemented
}

// DeductBalance settles a completed withdrawal (reduces both available and settled balance).
func (q *Queries) DeductBalance(ctx context.Context, arg DeductBalanceParams) error {
	return errNotImplemented
}

// GetAccountWithNilosByUserAndCurrency returns the full account row including Nilos fields.
func (q *Queries) GetAccountWithNilosByUserAndCurrency(ctx context.Context, userID uuid.UUID, currency string) (AccountWithNilos, error) {
	return AccountWithNilos{}, errNotImplemented
}

// ─── Migration 000007: business FX rates ─────────────────────────────────────

type UpsertBusinessFxRateParams struct {
	SourceCurrency string
	TargetCurrency string
	Rate           string // decimal e.g. "595.00"
	FeePercent     string // decimal e.g. "0.0100"
	FlatFee        string // decimal e.g. "0.00"
	SetBy          *uuid.UUID
}

type GetBusinessFxRateParams struct {
	SourceCurrency string
	TargetCurrency string
}

// UpsertBusinessFxRate creates or updates the conversion rate for a currency pair.
func (q *Queries) UpsertBusinessFxRate(ctx context.Context, arg UpsertBusinessFxRateParams) (BusinessFxRate, error) {
	return BusinessFxRate{}, errNotImplemented
}

// GetBusinessFxRate returns the active rate for a currency pair, or an error if not found.
func (q *Queries) GetBusinessFxRate(ctx context.Context, arg GetBusinessFxRateParams) (BusinessFxRate, error) {
	return BusinessFxRate{}, errNotImplemented
}

// ListBusinessFxRates returns all configured rates.
func (q *Queries) ListBusinessFxRates(ctx context.Context) ([]BusinessFxRate, error) {
	return nil, errNotImplemented
}

// ─── Migration 000007: beneficiaries ─────────────────────────────────────────

type CreateBeneficiaryParams struct {
	UserID              uuid.UUID
	Label               string
	Type                string // "mobile_money" | "bank"
	DestinationCurrency string
	Country             string
	PhoneNumber         *string
	Operator            *string
	BankName            *string
	AccountNumber       *string
	IBAN                *string
	SwiftCode           *string
	SortCode            *string
	RoutingNumber       *string
}

type GetBeneficiaryByIDParams struct {
	ID     uuid.UUID
	UserID uuid.UUID
}

func (q *Queries) CreateBeneficiary(ctx context.Context, arg CreateBeneficiaryParams) (Beneficiary, error) {
	return Beneficiary{}, errNotImplemented
}

func (q *Queries) ListBeneficiaries(ctx context.Context, userID uuid.UUID) ([]Beneficiary, error) {
	return nil, errNotImplemented
}

func (q *Queries) GetBeneficiaryByID(ctx context.Context, arg GetBeneficiaryByIDParams) (Beneficiary, error) {
	return Beneficiary{}, errNotImplemented
}

func (q *Queries) UpdateBeneficiaryNilosRecipient(ctx context.Context, id uuid.UUID, nilosRecipientID string) error {
	return errNotImplemented
}

func (q *Queries) DeleteBeneficiary(ctx context.Context, id, userID uuid.UUID) error {
	return errNotImplemented
}

// ─── Migration 000007: fiat withdrawals ──────────────────────────────────────

type CreateFiatWithdrawalParams struct {
	UserID              uuid.UUID
	SourceCurrency      string
	SourceAmount        string
	Fee                 string
	FeeCurrency         string
	FxRate              *string // nil if same currency
	DestinationType     string
	DestinationCurrency string
	DestinationAmount   string
	DestinationCountry  string
	RecipientName       string
	PhoneNumber         *string
	Operator            *string
	BankName            *string
	AccountNumber       *string
	IBAN                *string
	SwiftCode           *string
	SortCode            *string
	RoutingNumber       *string
	Reference           string
	BeneficiaryID       *uuid.UUID
}

type GetFiatWithdrawalByIDParams struct {
	ID     uuid.UUID
	UserID uuid.UUID
}

type ListFiatWithdrawalsParams struct {
	UserID uuid.UUID
	Limit  int32
	Offset int32
}

type UpdateFiatWithdrawalStatusParams struct {
	ID            uuid.UUID
	Status        string
	FailureReason *string
}

type UpdateFiatWithdrawalHub2RefParams struct {
	ID            uuid.UUID
	Hub2Reference string
	Status        string
}

type UpdateFiatWithdrawalNilosRefParams struct {
	ID               uuid.UUID
	NilosPayoutID    string
	NilosRecipientID string
	Status           string
}

func (q *Queries) CreateFiatWithdrawal(ctx context.Context, arg CreateFiatWithdrawalParams) (FiatWithdrawal, error) {
	return FiatWithdrawal{}, errNotImplemented
}

func (q *Queries) GetFiatWithdrawalByID(ctx context.Context, arg GetFiatWithdrawalByIDParams) (FiatWithdrawal, error) {
	return FiatWithdrawal{}, errNotImplemented
}

func (q *Queries) GetFiatWithdrawalByHub2Ref(ctx context.Context, hub2Ref string) (FiatWithdrawal, error) {
	return FiatWithdrawal{}, errNotImplemented
}

func (q *Queries) ListFiatWithdrawals(ctx context.Context, arg ListFiatWithdrawalsParams) ([]FiatWithdrawal, error) {
	return nil, errNotImplemented
}

func (q *Queries) CountFiatWithdrawals(ctx context.Context, userID uuid.UUID) (int64, error) {
	return 0, errNotImplemented
}

func (q *Queries) UpdateFiatWithdrawalStatus(ctx context.Context, arg UpdateFiatWithdrawalStatusParams) (FiatWithdrawal, error) {
	return FiatWithdrawal{}, errNotImplemented
}

func (q *Queries) UpdateFiatWithdrawalHub2Ref(ctx context.Context, arg UpdateFiatWithdrawalHub2RefParams) (FiatWithdrawal, error) {
	return FiatWithdrawal{}, errNotImplemented
}

func (q *Queries) UpdateFiatWithdrawalNilosRef(ctx context.Context, arg UpdateFiatWithdrawalNilosRefParams) (FiatWithdrawal, error) {
	return FiatWithdrawal{}, errNotImplemented
}

// ─── Migration 000008: security (2FA + PIN + biometrics) ─────────────────────

type SetTOTPSecretParams struct {
	ID         uuid.UUID
	TOTPSecret *string // nil to clear
}

type SetTOTPEnabledParams struct {
	ID          uuid.UUID
	TOTPEnabled bool
	TOTPSecret  *string // set when enabling, nil to clear when disabling
}

func (q *Queries) SetTOTPEnabled(ctx context.Context, arg SetTOTPEnabledParams) error {
	return errNotImplemented
}

func (q *Queries) SetBiometricsEnabled(ctx context.Context, userID uuid.UUID, enabled bool) error {
	return errNotImplemented
}

// ChangePIN updates the stored PIN hash for a user (requires old hash to have been
// verified by the caller before invoking this).
func (q *Queries) ChangePIN(ctx context.Context, id uuid.UUID, newPinHash string) error {
	return errNotImplemented
}

// ─── Migration 000008: user preferences ──────────────────────────────────────

type UpsertUserPreferencesParams struct {
	UserID            uuid.UUID
	Language          string
	DarkMode          string
	BiometricsEnabled bool
}

func (q *Queries) UpsertUserPreferences(ctx context.Context, arg UpsertUserPreferencesParams) (UserPreferences, error) {
	return UserPreferences{}, errNotImplemented
}

// ─── Migration 000008: support tickets ───────────────────────────────────────

type CreateSupportTicketParams struct {
	UserID    uuid.UUID
	Reference string
	Subject   string
	Category  string
}

type CreateSupportMessageParams struct {
	TicketID   uuid.UUID
	SenderType string
	SenderID   *uuid.UUID
	Body       string
}

type ListSupportTicketsParams struct {
	UserID uuid.UUID
	Limit  int32
	Offset int32
}

type GetSupportTicketParams struct {
	ID     uuid.UUID
	UserID uuid.UUID
}

func (q *Queries) CreateSupportTicket(ctx context.Context, arg CreateSupportTicketParams) (SupportTicket, error) {
	return SupportTicket{}, errNotImplemented
}

func (q *Queries) GetSupportTicket(ctx context.Context, arg GetSupportTicketParams) (SupportTicket, error) {
	return SupportTicket{}, errNotImplemented
}

func (q *Queries) ListSupportTickets(ctx context.Context, arg ListSupportTicketsParams) ([]SupportTicket, error) {
	return nil, errNotImplemented
}

func (q *Queries) CountSupportTickets(ctx context.Context, userID uuid.UUID) (int64, error) {
	return 0, errNotImplemented
}

func (q *Queries) CreateSupportMessage(ctx context.Context, arg CreateSupportMessageParams) (SupportMessage, error) {
	return SupportMessage{}, errNotImplemented
}

func (q *Queries) ListSupportMessages(ctx context.Context, ticketID uuid.UUID) ([]SupportMessage, error) {
	return nil, errNotImplemented
}

func (q *Queries) UpdateSupportTicketStatus(ctx context.Context, id uuid.UUID, status string) (SupportTicket, error) {
	return SupportTicket{}, errNotImplemented
}

// ─── Migration 000008: FAQs ───────────────────────────────────────────────────

func (q *Queries) ListFAQs(ctx context.Context, category string) ([]FAQ, error) {
	return nil, errNotImplemented
}

func (q *Queries) GetFAQ(ctx context.Context, id uuid.UUID) (FAQ, error) {
	return FAQ{}, errNotImplemented
}

// Admin FAQ management
type UpsertFAQParams struct {
	ID        *uuid.UUID
	Question  string
	Answer    string
	Category  string
	SortOrder int
}

func (q *Queries) UpsertFAQ(ctx context.Context, arg UpsertFAQParams) (FAQ, error) {
	return FAQ{}, errNotImplemented
}

func (q *Queries) DeleteFAQ(ctx context.Context, id uuid.UUID) error {
	return errNotImplemented
}

// ─── Wallet detail + overview queries ────────────────────────────────────────

// ListWalletTransactionsParams supports filtered + searchable transaction lists.
// TypeFilter values: "" (all) | "transfer_in" | "transfer_out" | "exchange" | "deposit" | "withdrawal"
type ListWalletTransactionsParams struct {
	AccountID  uuid.UUID
	TypeFilter string
	Search     string // partial match on description or reference
	Limit      int32
	Offset     int32
}

// WalletTxStatsRow is the aggregate returned by GetWalletTxStats.
type WalletTxStatsRow struct {
	TotalIn  float64
	TotalOut float64
	Count    int64
}

func (q *Queries) ListWalletTransactions(ctx context.Context, arg ListWalletTransactionsParams) ([]Transaction, error) {
	return nil, errNotImplemented
}

func (q *Queries) CountWalletTransactions(ctx context.Context, arg ListWalletTransactionsParams) (int64, error) {
	return 0, errNotImplemented
}

// GetWalletTxStats returns aggregate in/out totals and count for an account.
func (q *Queries) GetWalletTxStats(ctx context.Context, accountID uuid.UUID) (WalletTxStatsRow, error) {
	return WalletTxStatsRow{}, errNotImplemented
}

// ─── Exchange queries ─────────────────────────────────────────────────────────

type CreateFiatTransactionParams struct {
	ID          uuid.UUID
	AccountID   uuid.UUID
	Reference   string
	Type        string // "exchange" | "transfer_in" | "transfer_out"
	Amount      string // decimal string e.g. "500.00"
	Currency    string
	Fee         string // decimal string e.g. "0.00"
	Description *string
	Status      string
	Metadata    json.RawMessage
}

// CreateFiatTransaction inserts a single leg of a fiat operation (exchange, transfer).
func (q *Queries) CreateFiatTransaction(ctx context.Context, arg CreateFiatTransactionParams) (Transaction, error) {
	return Transaction{}, errNotImplemented
}

type ListExchangesByUserParams struct {
	UserID uuid.UUID
	Limit  int32
	Offset int32
}

// ExchangeHistoryRow is one row from the debit-leg of an exchange.
type ExchangeHistoryRow struct {
	Transaction
	// Enriched fields decoded from Metadata.
	FromCurrency  string
	ToCurrency    string
	FromAmount    float64
	ToAmount      float64
	Rate          float64
	NilosPayoutID string
}

// ListExchangesByUser returns the debit-leg exchange transactions for a user,
// newest first. Metadata contains the full pair info.
func (q *Queries) ListExchangesByUser(ctx context.Context, arg ListExchangesByUserParams) ([]Transaction, error) {
	return nil, errNotImplemented
}

func (q *Queries) CountExchangesByUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	return 0, errNotImplemented
}

type ExchangeStatsRow struct {
	TotalExchanges int64
	TotalVolume    float64 // sum of from_amount across all exchanges
	TotalFees      float64
}

// GetExchangeStats returns aggregate stats for the user's exchange history.
func (q *Queries) GetExchangeStats(ctx context.Context, userID uuid.UUID) (ExchangeStatsRow, error) {
	return ExchangeStatsRow{}, errNotImplemented
}
