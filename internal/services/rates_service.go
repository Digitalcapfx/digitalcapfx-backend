package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

// SupportedRateCurrencies are the fiat currencies DigitalFX quotes rates for.
var SupportedRateCurrencies = []string{"USD", "EUR", "GBP", "XAF", "XOF", "NGN"}

// IsSupportedRateCurrency reports whether c (case-insensitive) is quotable.
func IsSupportedRateCurrency(c string) bool {
	c = strings.ToUpper(strings.TrimSpace(c))
	for _, s := range SupportedRateCurrencies {
		if s == c {
			return true
		}
	}
	return false
}

// RatesService manages the admin-controlled per-currency buy/sell/standard rates.
type RatesService struct {
	pool *pgxpool.Pool
}

func NewRatesService(pool *pgxpool.Pool) *RatesService {
	return &RatesService{pool: pool}
}

// List returns all configured currency rates.
func (s *RatesService) List(ctx context.Context) ([]db.CurrencyRate, error) {
	return db.New(s.pool).ListCurrencyRates(ctx)
}

// Get returns the rate set for one currency.
func (s *RatesService) Get(ctx context.Context, currency string) (db.CurrencyRate, error) {
	return db.New(s.pool).GetCurrencyRate(ctx, strings.ToUpper(strings.TrimSpace(currency)))
}

// SetCurrencyRateInput carries an admin rate update.
type SetCurrencyRateInput struct {
	Currency     string
	StandardRate float64
	BuyRate      float64
	SellRate     float64
	AdminID      uuid.UUID
}

// Set upserts a currency's rates. Currency must be supported and all three rates
// must be positive.
func (s *RatesService) Set(ctx context.Context, in SetCurrencyRateInput) (db.CurrencyRate, error) {
	cur := strings.ToUpper(strings.TrimSpace(in.Currency))
	if !IsSupportedRateCurrency(cur) {
		return db.CurrencyRate{}, fmt.Errorf("unsupported currency %q (supported: %s)", in.Currency, strings.Join(SupportedRateCurrencies, ", "))
	}
	if in.StandardRate <= 0 || in.BuyRate <= 0 || in.SellRate <= 0 {
		return db.CurrencyRate{}, fmt.Errorf("standard_rate, buy_rate and sell_rate must all be greater than zero")
	}
	admin := in.AdminID
	return db.New(s.pool).UpsertCurrencyRate(ctx, db.UpsertCurrencyRateParams{
		Currency:     cur,
		StandardRate: in.StandardRate,
		BuyRate:      in.BuyRate,
		SellRate:     in.SellRate,
		UpdatedBy:    &admin,
	})
}
