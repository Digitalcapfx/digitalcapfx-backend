package services

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/caas"
	"github.com/rachfinance/digitalfx/internal/clients/hub2"
	"github.com/rachfinance/digitalfx/internal/clients/metamap"
	"github.com/rachfinance/digitalfx/internal/clients/nilos"
	"github.com/rachfinance/digitalfx/internal/clients/payments"
	"github.com/rachfinance/digitalfx/internal/clients/sumsub"
	"github.com/rachfinance/digitalfx/internal/config"
	"github.com/rachfinance/digitalfx/internal/kyc"
	"github.com/rachfinance/digitalfx/internal/pkg/email"
)

// Services bundles every domain service, passed to handlers as a single dependency.
type Services struct {
	Auth           *AuthService
	Account        *AccountService
	Wallet         *WalletService
	Crypto         *CryptoService
	KYC            *KYCService
	HUB2           *HUB2Service
	Dashboard      *DashboardService
	Notifications  *NotificationService
	Withdrawal     *WithdrawalService
	Security       *SecurityService
	Preferences    *PreferencesService
	Support        *SupportService
	WalletOverview *WalletOverviewService
	Exchange       *ExchangeService
	Activity       *ActivityService
	Insights       *InsightsService
	Staff          *StaffService
	Business       *BusinessService
	Referral       *ReferralService
	Swap           *SwapService
	Upload         *UploadService
	UserManagement *UserManagementService
	Limits         *LimitsService
}

func New(
	pool *pgxpool.Pool,
	rdb *redis.Client,
	paymentsClient *payments.Client,
	caasClient *caas.Client,
	hub2Client *hub2.Client,
	emailClient *email.Client,
	metamapClient *metamap.Client,
	nilosClient *nilos.Client,
	cfg *config.Config,
	logger *zap.Logger,
) *Services {
	notif := NewNotificationService(pool, logger)
	hub2Svc := NewHUB2Service(pool, hub2Client, caasClient, logger)
	limitsSvc := NewLimitsService(pool, DefaultLimitsResolver(), logger)
	withdrawalSvc := NewWithdrawalService(pool, hub2Client, nilosClient, notif, limitsSvc, logger)
	hub2Svc.SetWithdrawalService(withdrawalSvc)

	// KYC provider is selected via KYC_PROVIDER ("metamap" default | "sumsub").
	var kycProvider kyc.KYCProvider = kyc.NewMetaMapProvider(metamapClient, cfg.MetaMap.FlowID, cfg.MetaMap.WebhookSecret)
	if cfg.KYCProvider == "sumsub" {
		kycProvider = kyc.NewSumsubProvider(sumsub.New(
			cfg.Sumsub.AppToken, cfg.Sumsub.SecretKey, cfg.Sumsub.LevelName, cfg.Sumsub.WebhookSecret))
	}

	return &Services{
		Auth:           NewAuthService(pool, rdb, cfg, logger, emailClient),
		Account:        NewAccountService(pool, logger),
		Wallet:         NewWalletService(pool, paymentsClient, hub2Client, logger),
		Crypto:         NewCryptoService(pool, caasClient, hub2Client, logger),
		KYC:            NewKYCService(pool, cfg, logger, kycProvider, emailClient, notif, nilosClient),
		HUB2:           hub2Svc,
		Dashboard:      NewDashboardService(pool, nilosClient, paymentsClient, caasClient, logger),
		Notifications:  notif,
		Withdrawal:     withdrawalSvc,
		Security:       NewSecurityService(pool, rdb, logger),
		Preferences:    NewPreferencesService(pool, logger),
		Support:        NewSupportService(pool, logger),
		WalletOverview: NewWalletOverviewService(pool, caasClient, paymentsClient, logger),
		Exchange:       NewExchangeService(pool, nilosClient, logger),
		Activity:       NewActivityService(pool, logger),
		Insights:       NewInsightsService(pool, caasClient, paymentsClient, logger),
		Staff:          NewStaffService(pool, emailClient, cfg.App.BaseURL, logger),
		Business:       NewBusinessService(pool, logger),
		Referral:       NewReferralService(pool, logger),
		Swap:           NewSwapService(paymentsClient, logger),
		Upload:         NewUploadService(cfg, logger),
		Limits:         limitsSvc,
		UserManagement: NewUserManagementService(pool, logger),
	}
}
