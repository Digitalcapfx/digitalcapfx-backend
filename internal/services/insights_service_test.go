package services

import (
	"fmt"
	"testing"
	"time"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

// ── periodStart ───────────────────────────────────────────────────────────────

func TestPeriodStart(t *testing.T) {
	now := time.Now()

	cases := []struct {
		period  string
		wantMin time.Duration // minimum expected delta from now
		wantMax time.Duration // maximum expected delta from now
	}{
		{"1w", 6*24*time.Hour + 23*time.Hour, 7*24*time.Hour + time.Hour},
		{"1m", 29*24*time.Hour, 32*24*time.Hour},  // months differ in length
		{"3m", 88*24*time.Hour, 93*24*time.Hour},
		{"6m", 179*24*time.Hour, 185*24*time.Hour},
		{"", 29*24*time.Hour, 32*24*time.Hour},     // default is 1m
		{"bogus", 29*24*time.Hour, 32*24*time.Hour}, // unknown → 1m
	}

	for _, tc := range cases {
		t.Run("period="+tc.period, func(t *testing.T) {
			got := periodStart(tc.period)
			delta := now.Sub(got)
			if delta < tc.wantMin || delta > tc.wantMax {
				t.Errorf("periodStart(%q): delta=%v, want [%v, %v]", tc.period, delta, tc.wantMin, tc.wantMax)
			}
		})
	}
}

// ── computeTrendChange ────────────────────────────────────────────────────────

func TestComputeTrendChange_Empty(t *testing.T) {
	change, label := computeTrendChange(nil, 1000)
	if change != 0 {
		t.Errorf("empty pts: change=%v, want 0", change)
	}
	if label != "+0.0%" {
		t.Errorf("empty pts: label=%q, want '+0.0%%'", label)
	}
}

func TestComputeTrendChange_SinglePoint(t *testing.T) {
	pts := []BalanceTrendPoint{{TotalUSD: 1000}}
	change, label := computeTrendChange(pts, 1100)
	if change != 0 {
		t.Errorf("single point (< 2): change=%v, want 0", change)
	}
	if label != "+0.0%" {
		t.Errorf("single point: label=%q, want '+0.0%%'", label)
	}
}

func TestComputeTrendChange_ZeroStart(t *testing.T) {
	pts := []BalanceTrendPoint{{TotalUSD: 0}, {TotalUSD: 500}}
	change, label := computeTrendChange(pts, 500)
	if change != 0 {
		t.Errorf("zero start: change=%v, want 0", change)
	}
	if label != "+0.0%" {
		t.Errorf("zero start: label=%q", label)
	}
}

func TestComputeTrendChange_PositiveGrowth(t *testing.T) {
	// Start 100k → now 110.7k = +10.7%
	pts := []BalanceTrendPoint{
		{TotalUSD: 100_000},
		{TotalUSD: 105_000},
	}
	change, label := computeTrendChange(pts, 110_700)
	if change != 10.7 {
		t.Errorf("positive growth: change=%v, want 10.7", change)
	}
	if label != "+10.7%" {
		t.Errorf("positive growth: label=%q, want '+10.7%%'", label)
	}
}

func TestComputeTrendChange_NegativeGrowth(t *testing.T) {
	pts := []BalanceTrendPoint{
		{TotalUSD: 200_000},
		{TotalUSD: 180_000},
	}
	change, label := computeTrendChange(pts, 170_000)
	if change != -15.0 {
		t.Errorf("negative growth: change=%v, want -15.0", change)
	}
	if label != "-15.0%" {
		t.Errorf("negative growth: label=%q, want '-15.0%%'", label)
	}
}

func TestComputeTrendChange_NoChange(t *testing.T) {
	pts := []BalanceTrendPoint{{TotalUSD: 50_000}, {TotalUSD: 50_000}}
	change, label := computeTrendChange(pts, 50_000)
	if change != 0 {
		t.Errorf("no change: change=%v, want 0", change)
	}
	if label != "+0.0%" {
		t.Errorf("no change: label=%q, want '+0.0%%'", label)
	}
}

// ── buildTrendPoints ──────────────────────────────────────────────────────────

func TestBuildTrendPoints_WithData(t *testing.T) {
	since := time.Now().AddDate(0, 0, -7)
	rows := []db.BalanceTrendRow{
		{Date: since, FiatUSD: 10_000, CryptoUSD: 5_000},
		{Date: since.AddDate(0, 0, 3), FiatUSD: 11_000, CryptoUSD: 5_500},
		{Date: since.AddDate(0, 0, 7), FiatUSD: 12_000, CryptoUSD: 6_000},
	}
	pts := buildTrendPoints(rows, since, 12_000, 6_000)

	if len(pts) != 3 {
		t.Fatalf("expected 3 points, got %d", len(pts))
	}
	if pts[0].FiatUSD != 10_000 {
		t.Errorf("pts[0].FiatUSD: got %v, want 10000", pts[0].FiatUSD)
	}
	if pts[0].TotalUSD != 15_000 {
		t.Errorf("pts[0].TotalUSD: got %v, want 15000", pts[0].TotalUSD)
	}
	if pts[2].TotalUSD != 18_000 {
		t.Errorf("pts[2].TotalUSD: got %v, want 18000", pts[2].TotalUSD)
	}
	// Date formatting
	wantDate := since.Format("Jan 2")
	if pts[0].Date != wantDate {
		t.Errorf("pts[0].Date: got %q, want %q", pts[0].Date, wantDate)
	}
}

func TestBuildTrendPoints_FallbackLastPointIsCurrent(t *testing.T) {
	// No DB rows — fallback synthetic points. Last point must equal current balance.
	since := time.Now().AddDate(0, 0, -7)
	curFiat, curCrypto := 35_000.0, 83_000.0
	pts := buildTrendPoints(nil, since, curFiat, curCrypto)

	if len(pts) < 2 {
		t.Fatalf("fallback: expected at least 2 points, got %d", len(pts))
	}
	last := pts[len(pts)-1]
	if last.FiatUSD != curFiat {
		t.Errorf("last.FiatUSD: got %v, want %v", last.FiatUSD, curFiat)
	}
	if last.CryptoUSD != curCrypto {
		t.Errorf("last.CryptoUSD: got %v, want %v", last.CryptoUSD, curCrypto)
	}
	if last.TotalUSD != roundUSD(curFiat+curCrypto) {
		t.Errorf("last.TotalUSD: got %v, want %v", last.TotalUSD, roundUSD(curFiat+curCrypto))
	}
}

func TestBuildTrendPoints_FallbackStartIsLower(t *testing.T) {
	// First synthetic point should be less than current (90% / 88%).
	since := time.Now().AddDate(0, 0, -30)
	curFiat, curCrypto := 100_000.0, 50_000.0
	pts := buildTrendPoints(nil, since, curFiat, curCrypto)

	if len(pts) < 2 {
		t.Skip("not enough fallback points")
	}
	first := pts[0]
	if first.FiatUSD >= curFiat {
		t.Errorf("first fallback fiat (%v) should be < current (%v)", first.FiatUSD, curFiat)
	}
	if first.CryptoUSD >= curCrypto {
		t.Errorf("first fallback crypto (%v) should be < current (%v)", first.CryptoUSD, curCrypto)
	}
}

func TestBuildTrendPoints_FallbackZeroBalance(t *testing.T) {
	// Zero balance: all points should be zero.
	since := time.Now().AddDate(0, 0, -7)
	pts := buildTrendPoints(nil, since, 0, 0)
	for i, p := range pts {
		if p.TotalUSD != 0 {
			t.Errorf("pts[%d].TotalUSD: got %v, want 0 for zero-balance", i, p.TotalUSD)
		}
	}
}

// ── buildMonthlyFlow ──────────────────────────────────────────────────────────

func TestBuildMonthlyFlow_WithData(t *testing.T) {
	rows := []db.MonthlyFlowRow{
		{Month: "Jan", Income: 10_000, Spending: 3_000},
		{Month: "Feb", Income: 12_000, Spending: 4_000},
		{Month: "Mar", Income: 9_000, Spending: 2_500},
	}
	pts := buildMonthlyFlow(rows)
	if len(pts) != 3 {
		t.Fatalf("expected 3 points, got %d", len(pts))
	}
	if pts[0].Month != "Jan" {
		t.Errorf("pts[0].Month: got %q, want Jan", pts[0].Month)
	}
	if pts[0].Income != 10_000 {
		t.Errorf("pts[0].Income: got %v, want 10000", pts[0].Income)
	}
	if pts[1].Spending != 4_000 {
		t.Errorf("pts[1].Spending: got %v, want 4000", pts[1].Spending)
	}
}

func TestBuildMonthlyFlow_FallbackGives6Months(t *testing.T) {
	pts := buildMonthlyFlow(nil)
	if len(pts) != 6 {
		t.Fatalf("fallback: expected 6 months, got %d", len(pts))
	}
	for i, p := range pts {
		if p.Income != 0 {
			t.Errorf("fallback pts[%d].Income: got %v, want 0", i, p.Income)
		}
		if p.Spending != 0 {
			t.Errorf("fallback pts[%d].Spending: got %v, want 0", i, p.Spending)
		}
		if p.Month == "" {
			t.Errorf("fallback pts[%d].Month is empty", i)
		}
	}
}

func TestBuildMonthlyFlow_FallbackRecentMonths(t *testing.T) {
	now := time.Now()
	pts := buildMonthlyFlow(nil)

	// Last point (index 5) should be the current month.
	wantLast := now.Format("Jan")
	if pts[5].Month != wantLast {
		t.Errorf("fallback last month: got %q, want %q", pts[5].Month, wantLast)
	}
	// First point (index 0) should be 5 months ago.
	wantFirst := now.AddDate(0, -5, 0).Format("Jan")
	if pts[0].Month != wantFirst {
		t.Errorf("fallback first month: got %q, want %q", pts[0].Month, wantFirst)
	}
}

// ── buildSpendingByType ───────────────────────────────────────────────────────

func TestBuildSpendingByType_Empty(t *testing.T) {
	pts, total := buildSpendingByType(nil)
	if len(pts) != 4 {
		t.Fatalf("empty rows: expected 4 types, got %d", len(pts))
	}
	if total != 0 {
		t.Errorf("empty rows: total=%v, want 0", total)
	}
	for _, p := range pts {
		if p.TotalAmount != 0 {
			t.Errorf("type %s: TotalAmount=%v, want 0", p.Type, p.TotalAmount)
		}
	}
}

func TestBuildSpendingByType_Ordering(t *testing.T) {
	pts, _ := buildSpendingByType(nil)
	wantOrder := []string{"send", "exchange", "withdraw", "deposit"}
	for i, want := range wantOrder {
		if pts[i].Type != want {
			t.Errorf("pts[%d].Type: got %q, want %q", i, pts[i].Type, want)
		}
	}
}

func TestBuildSpendingByType_Labels(t *testing.T) {
	pts, _ := buildSpendingByType(nil)
	wantLabels := map[string]string{
		"send":     "Send",
		"exchange": "Exchange",
		"withdraw": "Withdraw",
		"deposit":  "Deposit",
	}
	for _, p := range pts {
		if want, ok := wantLabels[p.Type]; ok && p.Label != want {
			t.Errorf("type %q: label=%q, want %q", p.Type, p.Label, want)
		}
	}
}

func TestBuildSpendingByType_AggregationAcrossSources(t *testing.T) {
	rows := []db.SpendingByTypeRow{
		{TxType: "transfer_out", Source: "fiat", Total: 1_000},
		{TxType: "transfer_out", Source: "crypto", Total: 500},
		{TxType: "exchange", Source: "fiat", Total: 2_000},
		{TxType: "withdrawal", Source: "fiat", Total: 300},
	}
	pts, total := buildSpendingByType(rows)

	// send type
	var sendPt, exchangePt, withdrawPt SpendingByTypePoint
	for _, p := range pts {
		switch p.Type {
		case "send":
			sendPt = p
		case "exchange":
			exchangePt = p
		case "withdraw":
			withdrawPt = p
		}
	}

	if sendPt.FiatAmount != 1_000 {
		t.Errorf("send.FiatAmount: got %v, want 1000", sendPt.FiatAmount)
	}
	if sendPt.CryptoAmount != 500 {
		t.Errorf("send.CryptoAmount: got %v, want 500", sendPt.CryptoAmount)
	}
	if sendPt.TotalAmount != 1_500 {
		t.Errorf("send.TotalAmount: got %v, want 1500", sendPt.TotalAmount)
	}
	if exchangePt.FiatAmount != 2_000 {
		t.Errorf("exchange.FiatAmount: got %v, want 2000", exchangePt.FiatAmount)
	}
	if withdrawPt.TotalAmount != 300 {
		t.Errorf("withdraw.TotalAmount: got %v, want 300", withdrawPt.TotalAmount)
	}
	if total != 3_800 {
		t.Errorf("total: got %v, want 3800", total)
	}
}

func TestBuildSpendingByType_UnknownTypeIgnored(t *testing.T) {
	rows := []db.SpendingByTypeRow{
		{TxType: "unknown_tx_type", Source: "fiat", Total: 999_999},
	}
	pts, total := buildSpendingByType(rows)
	if total != 0 {
		t.Errorf("unknown type should be ignored, total=%v", total)
	}
	for _, p := range pts {
		if p.TotalAmount != 0 {
			t.Errorf("unknown type leaked into %s, amount=%v", p.Type, p.TotalAmount)
		}
	}
}

// ── formatNet ─────────────────────────────────────────────────────────────────

func TestFormatNet(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{13_480, "+$13,480"},
		{0, "+$0"},
		{-2_000, "-$2,000"},
		{1_234_567, "+$1,234,567"},
		{-500, "-$500"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%.0f", tc.v), func(t *testing.T) {
			got := formatNet(tc.v)
			if got != tc.want {
				t.Errorf("formatNet(%v) = %q, want %q", tc.v, got, tc.want)
			}
		})
	}
}
