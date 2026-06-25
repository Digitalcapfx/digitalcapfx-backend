package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/caas"
	"github.com/rachfinance/digitalfx/internal/clients/hub2"
	"github.com/rachfinance/digitalfx/internal/clients/metamap"
	"github.com/rachfinance/digitalfx/internal/clients/payments"
	"github.com/rachfinance/digitalfx/internal/config"
	"github.com/rachfinance/digitalfx/internal/pkg/email"
	"github.com/rachfinance/digitalfx/internal/services"
)

type Server struct {
	cfg    *config.Config
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

	// Service layer
	svc := services.New(pool, rdb, paymentsClient, caasClient, hub2Client, emailClient, metamapClient, cfg, logger)

	// Router
	r := newRouter(cfg, svc, logger)

	return &Server{
		cfg:    cfg,
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
