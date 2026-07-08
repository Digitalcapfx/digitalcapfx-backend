# Walkthrough: WaaS Integration, Swap & Nilos Auto-Provisioning

Full integration of Nilos virtual accounts and the Rach WaaS (Wallet-as-a-Service)
+ Swap endpoints into the DigitalFX codebase, plus the business-accounts,
teams, referrals, Sumsub-KYC, and market-data expansion.

---

## 1. Database queries & code generation

- sqlc is configured for **pgx/v5** (`sql_package: "pgx/v5"` in `sqlc.yaml`) with
  Go-initialism renames (TOTPEnabled, DeviceIP, AvatarURL, IPAddress, …) and
  pointer types for nullable uuid/timestamptz/date columns.
- All queries live in `internal/db/queries/*.sql`, one file per domain; run
  `make sqlc` after changing queries or migrations.
- `internal/db/sqlc/query.sql.go` holds hand-written stubs for features whose
  SQL is not written yet (they return `errNotImplemented`). Add the real query
  to `internal/db/queries/` and delete the stub when implementing.
- Hand-written composite types live in `custom_types.go` (UserFull,
  AccountWithNilos, FXRate, FAQ, …) and `admin.go` (StaffMember + converters).

## 2. Nilos auto-provisioning

- `internal/services/kyc_service.go` triggers background provisioning of
  EUR (SEPA), GBP (FPS), and USD (SWIFT) virtual accounts whenever a user's
  KYC status becomes `approved` (webhook or manual admin override).

## 3. Rach WaaS & Swap integration

- `internal/clients/payments/client.go` — full client for the payments service:
  wallets, derive/export, transfers, gas estimation, transactions, and the
  unified swap API (`GetSwapQuote`, `ExecuteSwap`, `GetSwapHistory`) mirroring
  the payments repo's `/api/v1/swap/*` endpoints (DEX + LiFi bridge, routed
  internally).
- `internal/services/wallet_service.go` — GetSwapQuote / ExecuteSwap /
  GetSwapHistory / GetWaasWallet / ExportPrivateKey / GetSeedPhrase /
  ListCustomerAddresses / EstimateGas / TransferCrypto / GetWaasTransactions,
  plus webhook helpers GetWalletByAddress / CreditWaasDeposit.
- Routes under `/api/v1/wallets`:
  - `GET  /wallets/swap/quote`
  - `POST /wallets/swap/execute`
  - `GET  /wallets/swap/history`
- `POST /webhooks/payments` — HMAC-verified deposit webhook that credits
  confirmed on-chain deposits and notifies the user.

## 4. KYC provider abstraction

- `internal/kyc` defines `KYCProvider` (Initiate / HandleWebhook / Name) with
  MetaMap and Sumsub implementations.
- Selected via `KYC_PROVIDER` env ("metamap" default | "sumsub");
  Sumsub credentials via `SUMSUB_APP_TOKEN`, `SUMSUB_SECRET_KEY`,
  `SUMSUB_LEVEL_NAME`, `SUMSUB_WEBHOOK_SECRET`.

## 5. Business accounts, teams & referrals

- Registration accepts `account_type` ("individual" | "business"); business
  signups collect company KYB fields and create a `business_profiles` row.
  Directors and documents are completed post-signup via `/api/v1/business/*`.
- Merchant teams: `/api/v1/team/*` with `LoadMerchantContext` +
  `RequireMerchantRole` middleware (owner implicit, roles enforced on
  invite / role-change / removal).
- Referrals & points: `GET /api/v1/referrals`,
  `GET /api/v1/referrals/points/ledger`.

## 6. Market data proxy

- `/api/v1/market/*` (REST, authed) and `/api/v1/market/ws` (WebSocket,
  `?key=` mapped to the internal Payments API key) reverse-proxy to the
  payments market-data service (`PAYMENTS_MARKET_DATA_URL`).

## 7. Withdrawal safety

- `checkDailyLimit` enforces a rolling 24-hour $10,000 USD limit across fiat
  and crypto withdrawals; wired into the fiat withdrawal `Initiate` flow.

---

## Verification

- `go build ./...` — clean
- `go vet ./...` — clean
- `go test ./...` — all packages pass

## Known gaps (stubs still returning errNotImplemented)

Support tickets/FAQs, beneficiaries, fiat withdrawal persistence queries,
FX-rate upserts, virtual cards, admin user-management queries, and several
exchange-history aggregates are still stubbed in `query.sql.go` / `admin.go`.
Each needs its SQL written in `internal/db/queries/` and the stub deleted.
