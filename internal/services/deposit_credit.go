package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

// creditDepositOnce idempotently credits an inbound provider deposit to the
// account ledger. The event is recorded (unique per provider + event_id) and the
// account credited in a single transaction, so a webhook redelivery never
// double-credits and a mid-flight crash never marks an event processed without
// crediting. Returns true only when a credit was actually applied (false for a
// duplicate).
func creditDepositOnce(ctx context.Context, pool *pgxpool.Pool, provider, eventID string, accountID uuid.UUID, currency string, amount float64) (bool, error) {
	if eventID == "" {
		return false, fmt.Errorf("deposit event id is required for idempotency")
	}
	var amt pgtype.Numeric
	if err := amt.Scan(fmt.Sprintf("%.6f", amount)); err != nil {
		return false, fmt.Errorf("encode deposit amount: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin deposit tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck — no-op after commit

	q := db.New(tx)
	acc := accountID
	rows, err := q.RecordDepositEvent(ctx, db.RecordDepositEventParams{
		Provider:  provider,
		EventID:   eventID,
		AccountID: &acc,
		Amount:    amt,
		Currency:  currency,
	})
	if err != nil {
		return false, fmt.Errorf("record deposit event: %w", err)
	}
	if rows == 0 {
		return false, nil // already processed
	}
	if _, err := q.CreditAccount(ctx, db.CreditAccountParams{ID: accountID, Balance: amt}); err != nil {
		return false, fmt.Errorf("credit account: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit deposit tx: %w", err)
	}
	return true, nil
}
