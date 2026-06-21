package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Server      ServerConfig
	Database    DatabaseConfig
	Redis       RedisConfig
	JWT         JWTConfig
	PaymentsAPI PaymentsAPIConfig
	CaaS        CaaSConfig
	HUB2        HUB2Config
	GCP         GCPConfig
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
	BaseURL string
	APIKey  string
}

// CaaSConfig points to the Rach CaaS service (abstraction wallets / P2P by phone).
type CaaSConfig struct {
	BaseURL string
	APIKey  string
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

	cfg.CaaS.BaseURL = require("CAAS_API_URL")
	cfg.CaaS.APIKey = require("CAAS_API_KEY")

	cfg.HUB2.BaseURL = getEnv("HUB2_BASE_URL", "https://api.hub2.io")
	cfg.HUB2.APIKey = require("HUB2_API_KEY")
	cfg.HUB2.SecretKey = require("HUB2_SECRET_KEY")
	cfg.HUB2.Mode = getEnv("HUB2_MODE", "sandbox")

	cfg.GCP.ProjectID = getEnv("GCP_PROJECT_ID", "")
	cfg.GCP.KYCBucket = getEnv("KYC_BUCKET", "")

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
