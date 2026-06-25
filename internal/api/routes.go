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
	authH      := handlers.NewAuthHandler(svc, cfg)
	profileH   := handlers.NewProfileHandler(svc)
	accountH   := handlers.NewAccountHandler(svc)
	walletH    := handlers.NewWalletHandler(svc)
	cryptoH    := handlers.NewCryptoHandler(svc)
	transferH  := handlers.NewTransferHandler(svc)
	kycH       := handlers.NewKYCHandler(svc)
	adminH     := handlers.NewAdminHandler(svc)
	dashboardH := handlers.NewDashboardHandler(svc)
	webhookH   := handlers.NewWebhookHandler(svc, cfg.HUB2.SecretKey, logger)

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
			r.Post("/google", authH.GoogleSignIn)
			r.Post("/token/refresh", authH.RefreshToken)
			r.Post("/forgot-pin", authH.ForgotPIN)
			r.Post("/reset-pin", authH.ResetPIN)
		})

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

				// Dashboard + activity feed
				r.Get("/dashboard", dashboardH.GetDashboard)
				r.Get("/activity", dashboardH.GetActivityFeed)
				r.Get("/crypto/contacts", dashboardH.GetRecentContacts)
			})

			// ── Admin routes (JWT + admin role) ─────────────────────────────
			r.Group(func(r chi.Router) {
				r.Use(middleware.AdminOnly)

				r.Get("/admin/kyc/pending", adminH.ListPendingKYC)
				r.Post("/admin/kyc/{id}/approve", adminH.ApproveKYC)
				r.Post("/admin/kyc/{id}/reject", adminH.RejectKYC)
			})
		})
	})

	return r
}
