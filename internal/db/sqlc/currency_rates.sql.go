// Currency rates queries (migration 000022). Hand-written to the same pgx style
// as the sqlc-generated files. Rates are returned as float8 for a clean numeric
// JSON API (FP precision is ample for FX display/quote rates).
package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CurrencyRate is one currency's admin-controlled rate set.
// standard_rate = display/mid rate; buy_rate = business buys at; sell_rate =
// business sells at. All expressed as units of this currency per 1 USD.
type CurrencyRate struct {
	Currency     string    `json:"currency"`
	StandardRate float64   `json:"standard_rate"`
	BuyRate      float64   `json:"buy_rate"`
	SellRate     float64   `json:"sell_rate"`
	UpdatedAt    time.Time `json:"updated_at"`
}

const upsertCurrencyRate = `-- name: UpsertCurrencyRate :one
INSERT INTO currency_rates (currency, standard_rate, buy_rate, sell_rate, updated_by, updated_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (currency) DO UPDATE SET
    standard_rate = EXCLUDED.standard_rate,
    buy_rate      = EXCLUDED.buy_rate,
    sell_rate     = EXCLUDED.sell_rate,
    updated_by    = EXCLUDED.updated_by,
    updated_at    = now()
RETURNING currency, standard_rate::float8, buy_rate::float8, sell_rate::float8, updated_at
`

type UpsertCurrencyRateParams struct {
	Currency     string
	StandardRate float64
	BuyRate      float64
	SellRate     float64
	UpdatedBy    *uuid.UUID
}

func (q *Queries) UpsertCurrencyRate(ctx context.Context, arg UpsertCurrencyRateParams) (CurrencyRate, error) {
	row := q.db.QueryRow(ctx, upsertCurrencyRate,
		arg.Currency, arg.StandardRate, arg.BuyRate, arg.SellRate, arg.UpdatedBy)
	var i CurrencyRate
	err := row.Scan(&i.Currency, &i.StandardRate, &i.BuyRate, &i.SellRate, &i.UpdatedAt)
	return i, err
}

const getCurrencyRate = `-- name: GetCurrencyRate :one
SELECT currency, standard_rate::float8, buy_rate::float8, sell_rate::float8, updated_at
FROM currency_rates WHERE currency = $1
`

func (q *Queries) GetCurrencyRate(ctx context.Context, currency string) (CurrencyRate, error) {
	row := q.db.QueryRow(ctx, getCurrencyRate, currency)
	var i CurrencyRate
	err := row.Scan(&i.Currency, &i.StandardRate, &i.BuyRate, &i.SellRate, &i.UpdatedAt)
	return i, err
}

const listCurrencyRates = `-- name: ListCurrencyRates :many
SELECT currency, standard_rate::float8, buy_rate::float8, sell_rate::float8, updated_at
FROM currency_rates ORDER BY currency
`

func (q *Queries) ListCurrencyRates(ctx context.Context) ([]CurrencyRate, error) {
	rows, err := q.db.Query(ctx, listCurrencyRates)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CurrencyRate{}
	for rows.Next() {
		var i CurrencyRate
		if err := rows.Scan(&i.Currency, &i.StandardRate, &i.BuyRate, &i.SellRate, &i.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}
