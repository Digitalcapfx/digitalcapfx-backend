package services

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/caas"
	"github.com/rachfinance/digitalfx/internal/clients/hub2"
	"github.com/rachfinance/digitalfx/internal/clients/metamap"
	"github.com/rachfinance/digitalfx/internal/clients/payments"
	"github.com/rachfinance/digitalfx/internal/config"
	"github.com/rachfinance/digitalfx/internal/pkg/email"
)

// Services bundles every domain service, passed to handlers as a single dependency.
type Services struct {
	Auth    *AuthService
	Account *AccountService
	Wallet  *WalletService
	Crypto  *CryptoService
	KYC     *KYCService
	HUB2    *HUB2Service
}

func New(
	pool *pgxpool.Pool,
	rdb *redis.Client,
	paymentsClient *payments.Client,
	caasClient *caas.Client,
	hub2Client *hub2.Client,
	emailClient *email.Client,
	metamapClient *metamap.Client,
	cfg *config.Config,
	logger *zap.Logger,
) *Services {
	return &Services{
		Auth:    NewAuthService(pool, rdb, cfg, logger, emailClient),
		Account: NewAccountService(pool, logger),
		Wallet:  NewWalletService(pool, paymentsClient, hub2Client, logger),
		Crypto:  NewCryptoService(pool, caasClient, hub2Client, logger),
		KYC:     NewKYCService(pool, cfg, logger, metamapClient, emailClient),
		HUB2:    NewHUB2Service(pool, hub2Client, caasClient, logger),
	}
}
