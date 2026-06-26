package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	httpSwagger "github.com/swaggo/http-swagger"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/api/handlers"
	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/config"
	"github.com/rachfinance/digitalfx/internal/services"
)

func newRouter(cfg *config.Config, svc *services.Services, pool *pgxpool.Pool, logger *zap.Logger) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Recoverer)
	r.Use(middleware.Logger(logger))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Handlers
	authH         := handlers.NewAuthHandler(svc, cfg)
	profileH      := handlers.NewProfileHandler(svc)
	accountH      := handlers.NewAccountHandler(svc)
	walletH       := handlers.NewWalletHandler(svc)
	cryptoH       := handlers.NewCryptoHandler(svc)
	transferH     := handlers.NewTransferHandler(svc)
	kycH          := handlers.NewKYCHandler(svc)
	adminH        := handlers.NewAdminHandler(svc)
	dashboardH    := handlers.NewDashboardHandler(svc)
	notificationH := handlers.NewNotificationHandler(svc)
	withdrawalH   := handlers.NewWithdrawalHandler(svc)
	securityH      := handlers.NewSecurityHandler(svc)
	prefsH         := handlers.NewPreferencesHandler(svc)
	supportH       := handlers.NewSupportHandler(svc)
	walletOverviewH := handlers.NewWalletOverviewHandler(svc)
	exchangeH       := handlers.NewExchangeHandler(svc)
	activityH       := handlers.NewActivityHandler(svc)
	insightsH       := handlers.NewInsightsHandler(svc)
	adminStaffH     := handlers.NewAdminStaffHandler(svc)
	adminUsersH     := handlers.NewAdminUsersHandler(svc)
	webhookH        := handlers.NewWebhookHandler(svc, cfg.HUB2.SecretKey, logger)

	kycRequired := middleware.KYCRequired(pool)

	// Health
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Public webhooks — no JWT required
	r.Post("/webhooks/hub2", webhookH.HUB2)
	r.Post("/webhooks/metamap", kycH.MetaMapWebhook)

	// Swagger UI
	r.Get("/swagger/*", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	))

	// API v1
	r.Route("/api/v1", func(r chi.Router) {

		// ── Public routes (no auth) ──────────────────────────────────────────
		r.Route("/auth", func(r chi.Router) {
			r.Post("/otp/send", authH.SendOTP)
			r.Post("/otp/verify", authH.VerifyOTP)
			r.Post("/register", authH.Register)
			r.Post("/login", authH.Login)
			r.Post("/2fa/login", authH.CompleteTOTPLogin)
			r.Post("/google", authH.GoogleSignIn)
			r.Post("/token/refresh", authH.RefreshToken)
			r.Post("/forgot-pin", authH.ForgotPIN)
			r.Post("/reset-pin", authH.ResetPIN)
		})

		// Support links — public (no auth needed for privacy policy / help center URLs)
		r.Get("/support/links", supportH.GetAppLinks)

		// ── Protected routes (JWT required) ─────────────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(cfg.JWT.Secret))

			// Auth — session & email management
			r.Post("/auth/logout", authH.Logout)
			r.Post("/auth/email/resend-otp", authH.SendEmailOTP)
			r.Post("/auth/email/verify", authH.VerifyEmail)
			r.Get("/auth/devices", authH.ListDevices)
			r.Delete("/auth/devices", authH.DisconnectAllDevices)
			r.Delete("/auth/devices/{id}", authH.DisconnectDevice)

			// Profile
			r.Get("/profile", profileH.GetProfile)
			r.Patch("/profile", profileH.UpdateProfile)
			r.Get("/profile/preferences", prefsH.Get)
			r.Patch("/profile/preferences", prefsH.Update)

			// Security — 2FA, PIN change, biometrics
			r.Get("/security", securityH.GetStatus)
			r.Post("/security/2fa/setup", securityH.Setup2FA)
			r.Post("/security/2fa/confirm", securityH.Confirm2FA)
			r.Delete("/security/2fa", securityH.Disable2FA)
			r.Post("/security/pin/change", securityH.ChangePIN)
			r.Post("/security/biometrics/enable", securityH.EnableBiometrics)
			r.Delete("/security/biometrics", securityH.DisableBiometrics)

			// Support — FAQs and tickets
			r.Get("/support/faqs", supportH.ListFAQs)
			r.Post("/support/tickets", supportH.CreateTicket)
			r.Get("/support/tickets", supportH.ListTickets)
			r.Get("/support/tickets/{id}", supportH.GetTicket)
			r.Post("/support/tickets/{id}/messages", supportH.ReplyToTicket)

			// KYC (status + doc upload available pre-approval; metamap init too)
			r.Route("/kyc", func(r chi.Router) {
				r.Get("/status", kycH.GetStatus)
				r.Get("/documents", kycH.ListDocuments)
				r.Post("/documents", kycH.UploadDocument)
				r.Post("/metamap/init", kycH.InitiateMetaMap)
			})

			// ── KYC-gated financial routes ───────────────────────────────────
			r.Group(func(r chi.Router) {
				r.Use(kycRequired)

				// Accounts (fiat — XAF, XOF, USD, GBP, EUR)
				r.Route("/accounts", func(r chi.Router) {
					r.Get("/", accountH.ListAccounts)
					r.Get("/{currency}", accountH.GetAccount)
					r.Get("/{currency}/transactions", accountH.GetTransactions)
					r.Get("/{currency}/transactions/{id}", accountH.GetTransaction)
				})

				// WaaS — custody wallets via Payments API
				r.Route("/wallets", func(r chi.Router) {
					r.Get("/", walletH.ListWallets)
					r.Post("/", walletH.CreateWallet)
					r.Get("/{walletId}/address", walletH.GetDepositAddress)
					r.Post("/deposit", walletH.InitiateDeposit)
					r.Post("/withdraw", walletH.InitiateWithdrawal)
				})

				// CaaS — Instant USD Account (ERC-4337 SCW)
				r.Route("/crypto", func(r chi.Router) {
					r.Get("/wallet", cryptoH.GetWallet)
					r.Post("/fund", cryptoH.FundAccount)
					r.Get("/balances", cryptoH.GetBalances)
					r.Post("/send", cryptoH.Send)
					r.Get("/transactions", cryptoH.ListTransactions)
					r.Get("/transactions/{id}", cryptoH.GetTransaction)
				})

				// Transfers (fiat internal)
				r.Route("/transfers", func(r chi.Router) {
					r.Post("/internal", transferH.InternalTransfer)
					r.Post("/hub2", transferH.Hub2Payment)
					r.Post("/exchange", transferH.ExchangeCurrency)
				})

				// Dashboard + activity feed + insights
				r.Get("/dashboard", dashboardH.GetDashboard)
				r.Get("/activity", activityH.GetFeed)
				r.Get("/insights", insightsH.GetInsights)
				r.Get("/crypto/contacts", dashboardH.GetRecentContacts)

				// ── Exchange ────────────────────────────────────────────────
				r.Get("/exchange/rate", exchangeH.GetRate)
				r.Post("/exchange/quote", exchangeH.GetQuote)
				r.Post("/exchange/execute", exchangeH.Execute)
				r.Get("/exchange/history", exchangeH.GetHistory)

				// ── Wallet overview + detail ────────────────────────────────
				r.Get("/wallets/overview", walletOverviewH.GetOverview)
				r.Get("/wallets/supported-assets", walletOverviewH.GetSupportedAssets)
				r.Get("/wallets/fiat/{currency}", walletOverviewH.GetFiatWalletDetail)
				r.Get("/wallets/fiat/{currency}/transactions", walletOverviewH.GetFiatTransactions)
				r.Get("/wallets/crypto/{network}", walletOverviewH.GetCryptoWalletDetail)
				r.Get("/wallets/crypto/{network}/transactions", walletOverviewH.GetCryptoTransactions)
				r.Get("/wallets/stablecoin/{symbol}", walletOverviewH.GetStablecoinDetail)
				r.Get("/wallets/stablecoin/{symbol}/transactions", walletOverviewH.GetStablecoinTransactions)

				// Fiat withdrawals (Nilos-backed accounts → bank or mobile money)
				r.Post("/withdrawals/quote", withdrawalH.Quote)
				r.Post("/withdrawals", withdrawalH.Initiate)
				r.Get("/withdrawals", withdrawalH.List)
				r.Get("/withdrawals/{id}", withdrawalH.Get)
				r.Get("/withdrawals/beneficiaries", withdrawalH.ListBeneficiaries)
				r.Post("/withdrawals/beneficiaries", withdrawalH.SaveBeneficiary)
				r.Delete("/withdrawals/beneficiaries/{id}", withdrawalH.DeleteBeneficiary)
			})

			// Notifications (no KYC gate — available from day 1)
			r.Get("/notifications", notificationH.List)
			r.Get("/notifications/unread-count", notificationH.UnreadCount)
			r.Patch("/notifications/read-all", notificationH.MarkAllRead)
			r.Patch("/notifications/{id}/read", notificationH.MarkRead)

			// ── Admin routes (JWT + staff permission check) ──────────────────
			r.Group(func(r chi.Router) {
				r.Use(middleware.LoadStaffPermissions(pool))

				// Dashboard + audit
				r.With(middleware.RequirePermission(services.PermAnalyticsView)).
					Get("/admin/dashboard", adminUsersH.AdminDashboard)
				r.With(middleware.RequirePermission(services.PermAuditView)).
					Get("/admin/audit-log", adminStaffH.GetAuditLog)

				// Staff — accept invite (any authenticated staff, no extra permission)
				r.Post("/admin/staff/invite/accept", adminStaffH.AcceptInvite)

				// Staff CRUD
				r.With(middleware.RequirePermission(services.PermStaffInvite)).
					Post("/admin/staff/invite", adminStaffH.InviteStaff)
				r.With(middleware.RequirePermission(services.PermStaffView)).
					Get("/admin/staff", adminStaffH.ListStaff)
				r.With(middleware.RequirePermission(services.PermStaffView)).
					Get("/admin/staff/{id}", adminStaffH.GetStaff)
				r.With(middleware.RequirePermission(services.PermStaffUpdate)).
					Patch("/admin/staff/{id}", adminStaffH.UpdateStaff)
				r.With(middleware.RequirePermission(services.PermStaffDisable)).
					Post("/admin/staff/{id}/disable", adminStaffH.DisableStaff)
				r.With(middleware.RequirePermission(services.PermStaffDisable)).
					Post("/admin/staff/{id}/enable", adminStaffH.EnableStaff)

				// Roles catalogue (read-only, any staff can see)
				r.With(middleware.RequirePermission(services.PermStaffView)).
					Get("/admin/roles", adminStaffH.ListRoles)
				r.With(middleware.RequirePermission(services.PermStaffView)).
					Get("/admin/roles/{name}", adminStaffH.GetRolePermissions)

				// User management
				r.With(middleware.RequirePermission(services.PermUsersView)).
					Get("/admin/users", adminUsersH.ListUsers)
				r.With(middleware.RequirePermission(services.PermUsersView)).
					Get("/admin/users/{id}", adminUsersH.GetUser)
				r.With(middleware.RequirePermission(services.PermUsersDisable)).
					Post("/admin/users/{id}/disable", adminUsersH.DisableUser)
				r.With(middleware.RequirePermission(services.PermUsersEnable)).
					Post("/admin/users/{id}/enable", adminUsersH.EnableUser)
				r.With(middleware.RequirePermission(services.PermUsersResetKYC)).
					Post("/admin/users/{id}/kyc/reset", adminUsersH.ResetUserKYC)
				r.With(middleware.RequirePermission(services.PermTxView)).
					Get("/admin/users/{id}/transactions", adminUsersH.ListUserTransactions)

				// KYC review
				r.With(middleware.RequirePermission(services.PermKYCView)).
					Get("/admin/kyc/pending", adminH.ListPendingKYC)
				r.With(middleware.RequirePermission(services.PermKYCApprove)).
					Post("/admin/kyc/{id}/approve", adminH.ApproveKYC)
				r.With(middleware.RequirePermission(services.PermKYCReject)).
					Post("/admin/kyc/{id}/reject", adminH.RejectKYC)

				// Withdrawal rates
				r.With(middleware.RequirePermission(services.PermWithdrawalsRates)).
					Post("/admin/withdrawal-rates", adminH.SetWithdrawalRate)
				r.With(middleware.RequirePermission(services.PermWithdrawalsView)).
					Get("/admin/withdrawal-rates", adminH.ListWithdrawalRates)
			})
		})
	})

	return r
}
