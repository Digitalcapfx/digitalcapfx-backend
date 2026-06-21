package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/api/handlers"
	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/config"
	"github.com/rachfinance/digitalfx/internal/services"
)

func newRouter(cfg *config.Config, svc *services.Services, logger *zap.Logger) http.Handler {
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
	authH := handlers.NewAuthHandler(svc, cfg)
	accountH := handlers.NewAccountHandler(svc)
	walletH := handlers.NewWalletHandler(svc)
	cryptoH := handlers.NewCryptoHandler(svc)
	transferH := handlers.NewTransferHandler(svc)
	kycH := handlers.NewKYCHandler(svc)

	// Health
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// API v1
	r.Route("/api/v1", func(r chi.Router) {

		// Public — no auth required
		r.Route("/auth", func(r chi.Router) {
			r.Post("/otp/send", authH.SendOTP)
			r.Post("/otp/verify", authH.VerifyOTP)
			r.Post("/register", authH.Register)
			r.Post("/login", authH.Login)
			r.Post("/token/refresh", authH.RefreshToken)
		})

		// Protected — JWT required
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(cfg.JWT.Secret))

			// Accounts (fiat — XAF, XOF, USD, GBP, EUR)
			r.Route("/accounts", func(r chi.Router) {
				r.Get("/", accountH.ListAccounts)
				r.Get("/{currency}", accountH.GetAccount)
				r.Get("/{currency}/transactions", accountH.GetTransactions)
				r.Get("/{currency}/transactions/{id}", accountH.GetTransaction)
			})

			// WaaS — crypto wallets via Payments API
			r.Route("/wallets", func(r chi.Router) {
				r.Get("/", walletH.ListWallets)
				r.Post("/", walletH.CreateWallet)
				r.Get("/{walletId}/address", walletH.GetDepositAddress)
				r.Post("/deposit", walletH.InitiateDeposit)
				r.Post("/withdraw", walletH.InitiateWithdrawal)
			})

			// CaaS — USDT/USDC P2P by phone number
			r.Route("/crypto", func(r chi.Router) {
				r.Get("/wallet", cryptoH.GetWallet)
				r.Get("/balances", cryptoH.GetBalances)
				r.Post("/send", cryptoH.Send)
				r.Get("/transactions", cryptoH.ListTransactions)
				r.Get("/transactions/{id}", cryptoH.GetTransaction)
			})

			// Transfers (fiat — internal between DigitalFX users)
			r.Route("/transfers", func(r chi.Router) {
				r.Post("/internal", transferH.InternalTransfer)
				r.Post("/hub2", transferH.Hub2Payment)
				r.Post("/exchange", transferH.ExchangeCurrency)
			})

			// KYC
			r.Route("/kyc", func(r chi.Router) {
				r.Get("/status", kycH.GetStatus)
				r.Get("/documents", kycH.ListDocuments)
				r.Post("/documents", kycH.UploadDocument)
			})
		})
	})

	return r
}
