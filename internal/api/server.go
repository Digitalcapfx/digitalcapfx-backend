package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/caas"
	"github.com/rachfinance/digitalfx/internal/clients/hub2"
	"github.com/rachfinance/digitalfx/internal/clients/metamap"
	"github.com/rachfinance/digitalfx/internal/clients/nilos"
	"github.com/rachfinance/digitalfx/internal/clients/nomba"
	"github.com/rachfinance/digitalfx/internal/clients/payments"
	"github.com/rachfinance/digitalfx/internal/config"
	"github.com/rachfinance/digitalfx/internal/pkg/email"
	"github.com/rachfinance/digitalfx/internal/pkg/sms"
	"github.com/rachfinance/digitalfx/internal/services"
)

type Server struct {
	cfg    *config.Config
	svc    *services.Services
	http   *http.Server
	logger *zap.Logger
}

func NewServer(cfg *config.Config) (*Server, error) {
	logger, _ := zap.NewProduction()
	if cfg.Server.Debug {
		logger, _ = zap.NewDevelopment()
	}

	// Database pool
	pool, err := pgxpool.New(context.Background(), cfg.Database.URL)
	if err != nil {
		return nil, fmt.Errorf("db pool: %w", err)
	}

	// Redis client
	opts, err := redis.ParseURL(cfg.Redis.URL)
	if err != nil {
		return nil, fmt.Errorf("redis url: %w", err)
	}
	rdb := redis.NewClient(opts)

	// External clients
	paymentsClient := payments.New(cfg.PaymentsAPI.APIKey, payments.WithBaseURL(cfg.PaymentsAPI.BaseURL))
	caasClient := caas.New(cfg.CaaS.APIKey, caas.WithBaseURL(cfg.CaaS.BaseURL))
	hub2Client := hub2.NewClient(cfg.HUB2.BaseURL, cfg.HUB2.APIKey, cfg.HUB2.SecretKey, cfg.HUB2.Mode)
	emailClient := email.New(
		cfg.Brevo.SMTPHost,
		cfg.Brevo.SMTPPort,
		cfg.Brevo.FromName,
		cfg.Brevo.FromEmail,
		cfg.Brevo.SMTPUser,
		cfg.Brevo.SMTPKey,
	)
	metamapClient := metamap.New(
		cfg.MetaMap.ClientID,
		cfg.MetaMap.ClientSecret,
		cfg.MetaMap.FlowID,
	)
	nilosClient := nilos.New(cfg.Nilos.APIKey, cfg.Nilos.APISecret,
		nilos.WithBaseURL(cfg.Nilos.BaseURL), nilos.WithOrgID(cfg.Nilos.OrgID))
	// Ops smoke test (gated by NILOS_DIAG=1): verify Nilos connectivity + account
	// creation on boot. Creates and immediately deletes a throwaway account so we
	// can confirm auth/org-scoping without touching real customer data. Off by default.
	if os.Getenv("NILOS_DIAG") == "1" {
		go runNilosDiag(nilosClient, logger)
	}
	nombaClient := nomba.New(cfg.Nomba.ClientID, cfg.Nomba.ClientSecret, cfg.Nomba.AccountID, nomba.WithBaseURL(cfg.Nomba.BaseURL))

	// SMS client (Brevo transactional SMS REST API v3).
	// smsClient is nil when no API key is configured (dev / test environments).
	var smsClient *sms.Client
	if cfg.Brevo.APIKey != "" {
		smsClient = sms.New(cfg.Brevo.APIKey, cfg.Brevo.SMSSenderName)
	}

	// Service layer
	svc := services.New(pool, rdb, paymentsClient, caasClient, hub2Client, emailClient, smsClient, metamapClient, nilosClient, nombaClient, cfg, logger)

	// Founder bootstrap: promote configured OWNER_PHONES to the "owner" role.
	svc.Auth.EnsureOwners(context.Background(), cfg.OwnerPhones)

	// Router
	r := newRouter(cfg, svc, pool, logger)

	return &Server{
		cfg:    cfg,
		svc:    svc,
		logger: logger,
		http: &http.Server{
			Addr:         ":" + cfg.Server.Port,
			Handler:      r,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
	}, nil
}

func (s *Server) Start(ctx context.Context) error {
	// Start background workers
	go s.svc.Market.Run(context.Background())

	errCh := make(chan error, 1)

	go func() {
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.logger.Info("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.http.Shutdown(shutdownCtx)
	}
}

// runNilosDiag is a one-shot ops smoke test (gated by NILOS_DIAG=1). It confirms
// that the Nilos credentials + org scoping actually authorise account creation —
// the operation that was returning 403 Forbidden. It creates a throwaway SEPA
// account, logs the outcome (IBAN on success, the API error on failure), then
// deletes it so no real data is left behind. Results appear under "NILOS DIAG".
func runNilosDiag(c *nilos.Client, logger *zap.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if accs, err := c.ListAccounts(ctx); err != nil {
		logger.Error("NILOS DIAG: ListAccounts FAILED", zap.Error(err))
	} else {
		logger.Info("NILOS DIAG: ListAccounts OK", zap.Int("count", len(accs)))
	}

	acc, err := c.CreateAccount(ctx, nilos.CreateAccountRequest{Name: "DigitalFX DIAG DELETE_ME", Rail: nilos.RailSEPA})
	if err != nil {
		logger.Error("NILOS DIAG: CreateAccount FAILED", zap.Error(err))
		return
	}
	logger.Info("NILOS DIAG: CreateAccount OK",
		zap.String("nilos_id", acc.ID),
		zap.String("iban", acc.DetailString("iban")),
		zap.String("bic", acc.DetailString("bic")))

	if err := c.DeleteAccount(ctx, acc.ID); err != nil {
		logger.Warn("NILOS DIAG: cleanup DeleteAccount failed — remove manually",
			zap.String("nilos_id", acc.ID), zap.Error(err))
	} else {
		logger.Info("NILOS DIAG: cleanup DeleteAccount OK", zap.String("nilos_id", acc.ID))
	}
}
