package services

import (
	"fmt"
	"testing"
	"time"
)

// ─── formatNumber ─────────────────────────────────────────────────────────────

func TestFormatNumber(t *testing.T) {
	cases := []struct {
		in       float64
		decimals int
		want     string
	}{
		{0, 2, "0.00"},
		{1000, 2, "1,000.00"},
		{12450.75, 2, "12,450.75"},
		{1234567.89, 2, "1,234,567.89"},
		{999, 0, "999"},
		{1000000, 0, "1,000,000"},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%v_%d", c.in, c.decimals), func(t *testing.T) {
			got := formatNumber(c.in, c.decimals)
			if got != c.want {
				t.Errorf("formatNumber(%v, %d) = %q, want %q", c.in, c.decimals, got, c.want)
			}
		})
	}
}

// ─── fiatFormatted ────────────────────────────────────────────────────────────

func TestFiatFormatted(t *testing.T) {
	cases := []struct {
		amount   float64
		currency string
		want     string
	}{
		{12450.75, "USD", "$12,450.75"},
		{9800.00, "EUR", "€9,800.00"},
		{7000.00, "GBP", "£7,000.00"},
		{650000, "XAF", "650,000 XAF"},
		{450000, "XOF", "450,000 XOF"},
	}
	for _, c := range cases {
		t.Run(c.currency, func(t *testing.T) {
			got := fiatFormatted(c.amount, c.currency)
			if got != c.want {
				t.Errorf("fiatFormatted(%v, %q) = %q, want %q", c.amount, c.currency, got, c.want)
			}
		})
	}
}

// ─── formatCrypto ─────────────────────────────────────────────────────────────

func TestFormatCrypto(t *testing.T) {
	cases := []struct {
		amount  float64
		network string
		want    string
	}{
		{0.45231234, "BTC", "0.4523 BTC"},
		{1.234, "ETH", "1.234 ETH"},
		{42.56, "SOL", "42.56 SOL"},
		{100.1234, "XRP", "100.1234 XRP"},
		{500.00, "POL", "500.00 POL"},
	}
	for _, c := range cases {
		t.Run(c.network, func(t *testing.T) {
			got := formatCrypto(c.amount, c.network)
			if got != c.want {
				t.Errorf("formatCrypto(%v, %q) = %q, want %q", c.amount, c.network, got, c.want)
			}
		})
	}
}

// ─── uiTypeToDBType ───────────────────────────────────────────────────────────

func TestUITypeToDBType(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"sent", "transfer_out"},
		{"received", "transfer_in"},
		{"exchanged", "exchange"},
		{"deposited", "deposit"},
		{"withdrawn", "withdrawal"},
		{"", ""},
		{"all", ""},
		{"SENT", "transfer_out"}, // case insensitive
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := uiTypeToDBType(c.in)
			if got != c.want {
				t.Errorf("uiTypeToDBType(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// ─── txPeriod ─────────────────────────────────────────────────────────────────

func TestTxPeriod(t *testing.T) {
	now := time.Now()
	// Start of the current week (Sunday).
	startOfWeek := now.AddDate(0, 0, -int(now.Weekday()))
	// Start of last week.
	startOfLastWeek := startOfWeek.AddDate(0, 0, -7)

	// 1 hour ago → this week (assuming we are not at the very start of the week)
	if int(now.Weekday()) > 0 { // not Sunday
		period, label := txPeriod(now.Add(-time.Hour))
		if period != "this_week" {
			t.Errorf("1 hour ago: got period %q, want this_week", period)
		}
		if label != "THIS WEEK" {
			t.Errorf("1 hour ago: got label %q, want THIS WEEK", label)
		}
	}

	// Middle of last week.
	lastWeekMid := startOfLastWeek.Add(3 * 24 * time.Hour)
	period, label := txPeriod(lastWeekMid)
	if period != "last_week" {
		t.Errorf("last week mid: got period %q, want last_week", period)
	}
	if label != "LAST WEEK" {
		t.Errorf("last week mid: got label %q, want LAST WEEK", label)
	}

	// 30 days ago → earlier.
	old := now.AddDate(0, 0, -30)
	period, label = txPeriod(old)
	if period != "earlier" {
		t.Errorf("30 days ago: got period %q, want earlier", period)
	}
	if label != "EARLIER" {
		t.Errorf("30 days ago: got label %q, want EARLIER", label)
	}
}

// ─── groupByPeriod ────────────────────────────────────────────────────────────

func TestGroupByPeriod(t *testing.T) {
	items := []WalletTxItem{
		{ID: "1", Period: "this_week"},
		{ID: "2", Period: "this_week"},
		{ID: "3", Period: "last_week"},
		{ID: "4", Period: "earlier"},
		{ID: "5", Period: "earlier"},
		{ID: "6", Period: "earlier"},
	}
	groups := groupByPeriod(items)

	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	if groups[0].Period != "this_week" || groups[0].Count != 2 {
		t.Errorf("group 0: got (%s, %d), want (this_week, 2)", groups[0].Period, groups[0].Count)
	}
	if groups[1].Period != "last_week" || groups[1].Count != 1 {
		t.Errorf("group 1: got (%s, %d), want (last_week, 1)", groups[1].Period, groups[1].Count)
	}
	if groups[2].Period != "earlier" || groups[2].Count != 3 {
		t.Errorf("group 2: got (%s, %d), want (earlier, 3)", groups[2].Period, groups[2].Count)
	}

	// Labels
	if groups[0].Label != "THIS WEEK" {
		t.Errorf("group 0 label = %q, want THIS WEEK", groups[0].Label)
	}
	if groups[1].Label != "LAST WEEK" {
		t.Errorf("group 1 label = %q, want LAST WEEK", groups[1].Label)
	}
	if groups[2].Label != "EARLIER" {
		t.Errorf("group 2 label = %q, want EARLIER", groups[2].Label)
	}
}

// ─── groupByPeriod — empty and single-group variants ─────────────────────────

func TestGroupByPeriod_Empty(t *testing.T) {
	groups := groupByPeriod(nil)
	if len(groups) != 0 {
		t.Errorf("expected 0 groups for empty input, got %d", len(groups))
	}
}

func TestGroupByPeriod_OnlyEarlier(t *testing.T) {
	items := []WalletTxItem{
		{ID: "1", Period: "earlier"},
		{ID: "2", Period: "earlier"},
	}
	groups := groupByPeriod(items)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Period != "earlier" {
		t.Errorf("expected earlier, got %s", groups[0].Period)
	}
}

// ─── currencyName ─────────────────────────────────────────────────────────────

func TestCurrencyName(t *testing.T) {
	cases := map[string]string{
		"USD": "US Dollar",
		"EUR": "Euro",
		"GBP": "British Pound",
		"XAF": "CFA Franc BEAC",
		"XOF": "CFA Franc BCEAO",
	}
	for cur, want := range cases {
		got := currencyName(cur)
		if got != want {
			t.Errorf("currencyName(%q) = %q, want %q", cur, got, want)
		}
	}
}

// ─── cryptoName ───────────────────────────────────────────────────────────────

func TestCryptoName(t *testing.T) {
	cases := map[string]string{
		"BTC": "Bitcoin",
		"ETH": "Ethereum",
		"SOL": "Solana",
		"LTC": "Litecoin",
		"TRX": "TRON",
		"POL": "Polygon",
	}
	for net, want := range cases {
		got := cryptoName(net)
		if got != want {
			t.Errorf("cryptoName(%q) = %q, want %q", net, got, want)
		}
	}
}

// ─── currencyFlag ─────────────────────────────────────────────────────────────

func TestCurrencyFlag(t *testing.T) {
	// Flags should be non-empty for known currencies.
	for _, cur := range []string{"USD", "EUR", "GBP", "XAF", "XOF"} {
		flag := currencyFlag(cur)
		if flag == "" {
			t.Errorf("currencyFlag(%q) returned empty string", cur)
		}
	}
}

// ─── defaultFXRates ───────────────────────────────────────────────────────────

func TestDefaultFXRates(t *testing.T) {
	rates := defaultFXRates()
	// USD should be 1.0.
	if rates["USD"] != 1.0 {
		t.Errorf("USD rate = %v, want 1.0", rates["USD"])
	}
	// All known currencies must be present and > 0.
	for _, cur := range []string{"USD", "EUR", "GBP", "XAF", "XOF", "BTC", "ETH", "SOL"} {
		r, ok := rates[cur]
		if !ok || r <= 0 {
			t.Errorf("defaultFXRates missing or zero for %q (got %v)", cur, r)
		}
	}
}

// ─── supportedNetworks ────────────────────────────────────────────────────────

func TestSupportedNetworks(t *testing.T) {
	nets := supportedNetworks()
	if len(nets) == 0 {
		t.Fatal("supportedNetworks returned empty slice")
	}
	for _, n := range []string{"BTC", "ETH", "SOL"} {
		found := false
		for _, got := range nets {
			if got == n {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("supportedNetworks: missing %q", n)
		}
	}
}
