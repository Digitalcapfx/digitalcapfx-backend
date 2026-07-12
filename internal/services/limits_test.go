package services

import "testing"

func TestLimitsResolver_For(t *testing.T) {
	r := DefaultLimitsResolver()

	cases := []struct {
		name     string
		input    string
		wantTier string
	}{
		{"business exact", "business", "business"},
		{"business mixed case", "Business", "business"},
		{"business padded", "  business  ", "business"},
		{"individual explicit", "individual", "individual"},
		{"empty defaults individual", "", "individual"},
		{"unknown defaults individual", "enterprise", "individual"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := r.For(c.input).Tier; got != c.wantTier {
				t.Fatalf("For(%q).Tier = %q, want %q", c.input, got, c.wantTier)
			}
		})
	}
}

// Business limits must be strictly more generous than individual across every
// dimension — that is the whole point of the tier.
func TestLimitsResolver_BusinessMoreGenerous(t *testing.T) {
	r := DefaultLimitsResolver()
	ind, bus := r.Individual, r.Business

	if !(bus.DailyWithdrawalUSD > ind.DailyWithdrawalUSD) {
		t.Errorf("daily withdrawal: business %.0f not > individual %.0f", bus.DailyWithdrawalUSD, ind.DailyWithdrawalUSD)
	}
	if !(bus.PerTransactionUSD > ind.PerTransactionUSD) {
		t.Errorf("per transaction: business %.0f not > individual %.0f", bus.PerTransactionUSD, ind.PerTransactionUSD)
	}
	if !(bus.MonthlyVolumeUSD > ind.MonthlyVolumeUSD) {
		t.Errorf("monthly volume: business %.0f not > individual %.0f", bus.MonthlyVolumeUSD, ind.MonthlyVolumeUSD)
	}
	if !(bus.MaxHoldingBalanceUSD > ind.MaxHoldingBalanceUSD) {
		t.Errorf("max holding: business %.0f not > individual %.0f", bus.MaxHoldingBalanceUSD, ind.MaxHoldingBalanceUSD)
	}
	if !(bus.DailyTransactionCount > ind.DailyTransactionCount) {
		t.Errorf("daily count: business %d not > individual %d", bus.DailyTransactionCount, ind.DailyTransactionCount)
	}
}
