package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// splitCSV splits a comma-separated env value into a trimmed, non-empty slice.
func splitCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

type Config struct {
	App         AppConfig
	Server      ServerConfig
	Database    DatabaseConfig
	Redis       RedisConfig
	JWT         JWTConfig
	PaymentsAPI PaymentsAPIConfig
	CaaS        CaaSConfig
	HUB2        HUB2Config
	GCP         GCPConfig
	Brevo       BrevoConfig
	MetaMap     MetaMapConfig
	Sumsub      SumsubConfig
	KYCProvider string // "metamap" (default) | "sumsub"
	Google      GoogleConfig
	Nilos       NilosConfig
	// OwnerPhones lists the phone numbers that should be promoted to the "owner"
	// staff role on startup (comma-separated OWNER_PHONES env). This is the only
	// way the founder account is designated — set at deploy time only.
	OwnerPhones []string
	// DefaultCountryCode is the numeric calling code (e.g. "234") used to expand
	// national, 0-prefixed phone numbers into canonical E.164. E.164 inputs work
	// for any country regardless of this. Env: DEFAULT_COUNTRY_CODE.
	DefaultCountryCode string
	// FirebaseCredentialsJSON is a service-account key for Firebase Cloud
	// Messaging (push). Leave empty to use Application Default Credentials
	// (Cloud Run workload identity). Env: FIREBASE_CREDENTIALS_JSON.
	FirebaseCredentialsJSON string
}

type AppConfig struct {
	Name    string
	BaseURL string
}

type ServerConfig struct {
	Port  string
	Env   string
	Debug bool
}

type DatabaseConfig struct {
	URL      string
	MaxConns int32
	MinConns int32
}

type RedisConfig struct {
	URL string
}

type JWTConfig struct {
	Secret        string
	AccessExpiry  time.Duration
	RefreshExpiry time.Duration
}

// PaymentsAPIConfig points to the Rach Payments service (WaaS).
type PaymentsAPIConfig struct {
	BaseURL       string
	APIKey        string
	WebhookSecret string
	MarketDataURL string
}

// CaaSConfig points to the Rach CaaS service (abstraction wallets / P2P by phone).
type CaaSConfig struct {
	BaseURL       string
	APIKey        string
	WebhookSecret string // HMAC-SHA256 secret for verifying inbound CaaS webhooks
}

// HUB2Config is the local payment gateway for XAF/XOF mobile money.
type HUB2Config struct {
	BaseURL   string
	APIKey    string
	SecretKey string
	Mode      string // sandbox | production
}

type GCPConfig struct {
	ProjectID string
	KYCBucket string
	// UploadsBucket is the GCS bucket for user uploads (KYC docs, avatars, …).
	UploadsBucket string
	// SignerSA is the service-account email used to sign V4 URLs via the IAM
	// SignBlob API (no key file needed). On Cloud Run this is the runtime SA.
	SignerSA string
}

// BrevoConfig holds Brevo credentials for transactional email (SMTP) and SMS (REST API v3).
// SMTP host: smtp-relay.brevo.com, port 587 (STARTTLS).
type BrevoConfig struct {
	SMTPHost      string
	SMTPPort      int
	FromName      string
	FromEmail     string
	SMTPUser      string // Brevo login email
	SMTPKey       string // Brevo SMTP API key
	APIKey        string // Brevo REST API key (v3) — used for transactional SMS
	SMSSenderName string // Alphanumeric sender ID shown on SMS recipient's phone (max 11 chars)
}

// MetaMapConfig holds credentials for MetaMap identity verification.
type MetaMapConfig struct {
	ClientID      string
	ClientSecret  string
	FlowID        string
	WebhookSecret string
}

// SumsubConfig holds credentials for Sumsub identity verification.
type SumsubConfig struct {
	AppToken      string
	SecretKey     string
	LevelName     string
	WebhookSecret string
}

// GoogleConfig holds credentials for Google Sign-In token verification.
type GoogleConfig struct {
	ClientID string // OAuth2 client ID — used to validate aud claim
}

// NilosConfig holds credentials for the Nilos fiat banking API.
// Auth uses HMAC-SHA256: X-Api-Key = APIKey (ID), X-Api-Signature = HMAC-SHA256(path+body, APISecret).
type NilosConfig struct {
	BaseURL   string
	APIKey    string // key ID sent as X-Api-Key
	APISecret string // signing secret for HMAC-SHA256
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	var errs []string
	require := func(key string) string {
		v := os.Getenv(key)
		if v == "" {
			errs = append(errs, fmt.Sprintf("missing required env var: %s", key))
		}
		return v
	}

	cfg := &Config{}

	cfg.App.Name = getEnv("APP_NAME", "DigitalFX")
	cfg.App.BaseURL = getEnv("APP_BASE_URL", "https://api.digitalfx.finance")

	cfg.Server.Port = getEnv("PORT", "8080")
	cfg.Server.Env = getEnv("ENV", "development")
	cfg.Server.Debug = getEnv("DEBUG", "false") == "true"

	cfg.Database.URL = require("DATABASE_URL")
	cfg.Database.MaxConns = int32(getEnvInt("DB_MAX_CONNS", 25))
	cfg.Database.MinConns = int32(getEnvInt("DB_MIN_CONNS", 5))

	cfg.Redis.URL = getEnv("REDIS_URL", "redis://localhost:6379/0")

	cfg.JWT.Secret = require("JWT_SECRET")
	cfg.JWT.AccessExpiry = time.Duration(getEnvInt("JWT_ACCESS_EXPIRY_MINUTES", 30)) * time.Minute
	cfg.JWT.RefreshExpiry = time.Duration(getEnvInt("JWT_REFRESH_EXPIRY_DAYS", 7)) * 24 * time.Hour

	cfg.PaymentsAPI.BaseURL = require("PAYMENTS_API_URL")
	cfg.PaymentsAPI.APIKey = require("PAYMENTS_API_KEY")
	cfg.PaymentsAPI.WebhookSecret = getEnv("PAYMENTS_WEBHOOK_SECRET", "")
	cfg.PaymentsAPI.MarketDataURL = getEnv("PAYMENTS_MARKET_DATA_URL", "http://localhost:8090")

	cfg.CaaS.BaseURL = require("CAAS_API_URL")
	cfg.CaaS.APIKey = require("CAAS_API_KEY")
	cfg.CaaS.WebhookSecret = getEnv("CAAS_WEBHOOK_SECRET", "")

	cfg.HUB2.BaseURL = getEnv("HUB2_BASE_URL", "https://api.hub2.io")
	cfg.HUB2.APIKey = require("HUB2_API_KEY")
	cfg.HUB2.SecretKey = require("HUB2_SECRET_KEY")
	cfg.HUB2.Mode = getEnv("HUB2_MODE", "sandbox")

	cfg.GCP.ProjectID = getEnv("GCP_PROJECT_ID", "")
	cfg.GCP.KYCBucket = getEnv("KYC_BUCKET", "")
	cfg.GCP.UploadsBucket = getEnv("UPLOADS_BUCKET", cfg.GCP.KYCBucket)
	cfg.GCP.SignerSA = getEnv("GCP_SIGNER_SA", "")

	cfg.Brevo.SMTPHost = getEnv("BREVO_SMTP_HOST", "smtp-relay.brevo.com")
	cfg.Brevo.SMTPPort = getEnvInt("BREVO_SMTP_PORT", 587)
	cfg.Brevo.FromName = getEnv("BREVO_FROM_NAME", "DigitalFX")
	cfg.Brevo.FromEmail = getEnv("BREVO_FROM_EMAIL", "noreply@digitalfx.finance")
	cfg.Brevo.SMTPUser = getEnv("BREVO_SMTP_USER", "")
	cfg.Brevo.SMTPKey = getEnv("BREVO_SMTP_KEY", "")
	cfg.Brevo.APIKey = getEnv("BREVO_API_KEY", "")        // REST v3 key for transactional SMS
	cfg.Brevo.SMSSenderName = getEnv("BREVO_SMS_SENDER", "DigitalFX") // max 11 chars

	cfg.MetaMap.ClientID = getEnv("METAMAP_CLIENT_ID", "")
	cfg.MetaMap.ClientSecret = getEnv("METAMAP_CLIENT_SECRET", "")
	cfg.MetaMap.FlowID = getEnv("METAMAP_FLOW_ID", "")
	cfg.MetaMap.WebhookSecret = getEnv("METAMAP_WEBHOOK_SECRET", "")

	cfg.Sumsub.AppToken = getEnv("SUMSUB_APP_TOKEN", "")
	cfg.Sumsub.SecretKey = getEnv("SUMSUB_SECRET_KEY", "")
	cfg.Sumsub.LevelName = getEnv("SUMSUB_LEVEL_NAME", "basic-kyc-level")
	cfg.Sumsub.WebhookSecret = getEnv("SUMSUB_WEBHOOK_SECRET", "")
	cfg.KYCProvider = getEnv("KYC_PROVIDER", "metamap")

	cfg.OwnerPhones = splitCSV(getEnv("OWNER_PHONES", ""))
	cfg.DefaultCountryCode = getEnv("DEFAULT_COUNTRY_CODE", "")
	cfg.FirebaseCredentialsJSON = getEnv("FIREBASE_CREDENTIALS_JSON", "")

	cfg.Google.ClientID = getEnv("GOOGLE_CLIENT_ID", "")

	cfg.Nilos.BaseURL = getEnv("NILOS_BASE_URL", "https://app-demo.nilos.io")
	cfg.Nilos.APIKey = getEnv("NILOS_API_KEY", "")
	cfg.Nilos.APISecret = getEnv("NILOS_API_SECRET", "")

	if len(errs) > 0 {
		return nil, fmt.Errorf("config errors:\n  %s", joinErrors(errs))
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func joinErrors(errs []string) string {
	s := ""
	for i, e := range errs {
		if i > 0 {
			s += "\n  "
		}
		s += e
	}
	return s
}
