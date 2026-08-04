package services

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/caas"
	"github.com/rachfinance/digitalfx/internal/clients/hub2"
	"github.com/rachfinance/digitalfx/internal/clients/metamap"
	"github.com/rachfinance/digitalfx/internal/clients/nilos"
	"github.com/rachfinance/digitalfx/internal/clients/nomba"
	"github.com/rachfinance/digitalfx/internal/clients/payments"
	"github.com/rachfinance/digitalfx/internal/clients/sumsub"
	"github.com/rachfinance/digitalfx/internal/config"
	"github.com/rachfinance/digitalfx/internal/kyc"
	"github.com/rachfinance/digitalfx/internal/pkg/email"
	"github.com/rachfinance/digitalfx/internal/pkg/sms"
)

// Services bundles every domain service, passed to handlers as a single dependency.
type Services struct {
	Auth           *AuthService
	Account        *AccountService
	Wallet         *WalletService
	CaaS           *CaaSService // Stablecoin (USDC) — EIP-4337 SCW rail
	KYC            *KYCService
	Nomba          *NombaService // Nigerian NGN rails (virtual accounts + payouts)
	Nilos          *NilosService // Nilos fiat rails: deposit crediting + live balance
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
	Department     *DepartmentService
	Business       *BusinessService
	Referral       *ReferralService
	Swap           *SwapService
	Market         *MarketService
	Upload         *UploadService
	UserManagement *UserManagementService
	Cards          *CardService
	VTU            *VTUService
	Limits         *LimitsService
	Rates          *RatesService
	ManualMomo     *ManualMomoService // manual mobile-money rails (parallel to Hub2)
}

func New(
	pool *pgxpool.Pool,
	rdb *redis.Client,
	paymentsClient *payments.Client,
	caasClient *caas.Client,
	hub2Client *hub2.Client,
	emailClient *email.Client,
	smsClient *sms.Client,
	metamapClient *metamap.Client,
	nilosClient *nilos.Client,
	nombaClient *nomba.Client,
	cfg *config.Config,
	logger *zap.Logger,
) *Services {
	// Configure phone-number canonicalisation before any normalization runs.
	SetDefaultCallingCode(cfg.DefaultCountryCode)

	// Firebase Cloud Messaging (mobile push). Optional: if init fails (e.g. no
	// credentials in local dev) push is disabled and only in-app notifications
	// are recorded.
	var fcm *FCMService
	if f, err := NewFCMService(context.Background(), cfg.FirebaseCredentialsJSON, logger); err != nil {
		logger.Warn("FCM push disabled — Firebase not initialised", zap.Error(err))
	} else {
		fcm = f
		logger.Info("FCM push notifications enabled")
	}

	notif := NewNotificationService(pool, fcm, logger)
	nilosSvc := NewNilosService(pool, nilosClient, notif, cfg.Nilos.WebhookSecret, logger)
	hub2Svc := NewHUB2Service(pool, hub2Client, caasClient, logger)
	limitsSvc := NewLimitsService(pool, DefaultLimitsResolver(), logger)
	withdrawalSvc := NewWithdrawalService(pool, hub2Client, nilosClient, nombaClient, notif, limitsSvc, logger)
	hub2Svc.SetWithdrawalService(withdrawalSvc)

	// KYC provider is selected via KYC_PROVIDER ("metamap" default | "sumsub").
	var kycProvider kyc.KYCProvider = kyc.NewMetaMapProvider(metamapClient, cfg.MetaMap.FlowID, cfg.MetaMap.WebhookSecret)
	if cfg.KYCProvider == "sumsub" {
		kycProvider = kyc.NewSumsubProvider(sumsub.New(
			cfg.Sumsub.AppToken, cfg.Sumsub.SecretKey, cfg.Sumsub.LevelName, cfg.Sumsub.WebhookSecret))
	}

	return &Services{
		Auth:           NewAuthService(pool, rdb, cfg, logger, emailClient, smsClient),
		Account:        NewAccountService(pool, logger),
		Wallet:         NewWalletService(pool, paymentsClient, hub2Client, logger),
		CaaS:           NewCaaSService(pool, caasClient, hub2Client, logger),
		KYC:            NewKYCService(pool, cfg, logger, kycProvider, emailClient, notif, nilosClient, nombaClient),
		Nomba:          NewNombaService(pool, nombaClient, notif, cfg.Nomba.WebhookSecret, logger),
		Nilos:          nilosSvc,
		HUB2:           hub2Svc,
		Dashboard:      NewDashboardService(pool, nilosClient, paymentsClient, caasClient, logger),
		Notifications:  notif,
		Withdrawal:     withdrawalSvc,
		Security:       NewSecurityService(pool, rdb, logger),
		Preferences:    NewPreferencesService(pool, logger),
		Support:        NewSupportService(pool, logger),
		WalletOverview: NewWalletOverviewService(pool, caasClient, paymentsClient, nilosSvc, logger),
		Exchange:       NewExchangeService(pool, nilosClient, logger),
		Activity:       NewActivityService(pool, logger),
		Insights:       NewInsightsService(pool, caasClient, paymentsClient, logger),
		Staff:          NewStaffService(pool, emailClient, cfg.App.BaseURL, logger),
		Department:     NewDepartmentService(pool, logger),
		Business:       NewBusinessService(pool, logger),
		Referral:       NewReferralService(pool, logger),
		Swap:           NewSwapService(paymentsClient, logger),
		Upload:         NewUploadService(cfg, logger),
		Limits:         limitsSvc,
		UserManagement: NewUserManagementService(pool, logger),
		Cards:          NewCardService(pool, logger),
		VTU:            NewVTUService(pool, hub2Client, logger),
		Market:         NewMarketService(cfg, logger, notif),
		Rates:          NewRatesService(pool),
		ManualMomo:     NewManualMomoService(pool, notif, emailClient, cfg.App.AdminNotifyEmail, cfg.App.BaseURL, logger),
	}
}
