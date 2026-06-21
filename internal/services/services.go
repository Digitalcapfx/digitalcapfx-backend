package services

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/caas"
	"github.com/rachfinance/digitalfx/internal/clients/hub2"
	"github.com/rachfinance/digitalfx/internal/clients/payments"
	"github.com/rachfinance/digitalfx/internal/config"
)

// Services bundles every domain service, passed to handlers as a single dependency.
type Services struct {
	Auth     *AuthService
	Account  *AccountService
	Wallet   *WalletService
	Crypto   *CryptoService
	KYC      *KYCService
	HUB2     *HUB2Service
}

func New(
	pool *pgxpool.Pool,
	rdb *redis.Client,
	paymentsClient *payments.Client,
	caasClient *caas.Client,
	hub2Client *hub2.Client,
	cfg *config.Config,
	logger *zap.Logger,
) *Services {
	return &Services{
		Auth:    NewAuthService(pool, rdb, cfg, logger),
		Account: NewAccountService(pool, logger),
		Wallet:  NewWalletService(pool, paymentsClient, logger),
		Crypto:  NewCryptoService(pool, caasClient, logger),
		KYC:     NewKYCService(pool, cfg, logger),
		HUB2:    NewHUB2Service(pool, hub2Client, logger),
	}
}
