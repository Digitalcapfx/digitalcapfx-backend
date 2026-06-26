package services

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/clients/nilos"
	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

// ── isSupportedCurrency ───────────────────────────────────────────────────────

func TestIsSupportedCurrency(t *testing.T) {
	supported := []string{"USD", "EUR", "GBP", "XAF", "XOF"}
	for _, c := range supported {
		t.Run(c, func(t *testing.T) {
			if !isSupportedCurrency(c) {
				t.Errorf("isSupportedCurrency(%q) = false, want true", c)
			}
		})
	}

	unsupported := []string{"BTC", "ETH", "USDC", "CAD", "JPY", "", "usd", "eur"}
	for _, c := range unsupported {
		t.Run("unsupported_"+c, func(t *testing.T) {
			if isSupportedCurrency(c) {
				t.Errorf("isSupportedCurrency(%q) = true, want false", c)
			}
		})
	}
}

// ── currencyRail ──────────────────────────────────────────────────────────────

func TestCurrencyRail(t *testing.T) {
	cases := []struct {
		currency, wantRail string
	}{
		{"USD", nilos.RailSWIFT},
		{"EUR", nilos.RailSEPA},
		{"GBP", nilos.RailFPS},
		{"XAF", nilos.RailCEMACBank},
		{"XOF", nilos.RailUEMOA},
		{"UNKNOWN", nilos.RailSWIFT}, // default
	}
	for _, tc := range cases {
		t.Run(tc.currency, func(t *testing.T) {
			got := currencyRail(tc.currency)
			if got != tc.wantRail {
				t.Errorf("currencyRail(%q) = %q, want %q", tc.currency, got, tc.wantRail)
			}
		})
	}
}

// ── fallbackRate ──────────────────────────────────────────────────────────────

func TestFallbackRate_SelfToSelf(t *testing.T) {
	// USD → USD should be 1.0
	rate := fallbackRate("USD", "USD")
	if math.Abs(rate-1.0) > 0.001 {
		t.Errorf("USD→USD fallback rate = %v, want ~1.0", rate)
	}
}

func TestFallbackRate_USDToEUR(t *testing.T) {
	// USD → EUR: from rates, USD is the base at 1.0, EUR is 0.925926 per USD.
	// fallbackRate: fromUSD = 1/rates[USD] = 1/1.0 = 1; toUSD = rates[EUR] = 0.925926; result = 0.925926
	rate := fallbackRate("USD", "EUR")
	if rate <= 0 || rate >= 1.5 {
		t.Errorf("USD→EUR fallback rate = %v, expected ~0.92", rate)
	}
}

func TestFallbackRate_EURToUSD(t *testing.T) {
	usdToEur := fallbackRate("USD", "EUR")
	eurToUsd := fallbackRate("EUR", "USD")
	// Product should be ~1.0 (inverse).
	product := usdToEur * eurToUsd
	if math.Abs(product-1.0) > 0.01 {
		t.Errorf("USD→EUR * EUR→USD = %v, want ~1.0", product)
	}
}

func TestFallbackRate_XAFToUSD(t *testing.T) {
	// defaultFXRates["XAF"] = 609 (XAF per USD). USD→XAF = 609/1 = 609.
	rate := fallbackRate("USD", "XAF")
	if rate < 500 || rate > 800 {
		t.Errorf("USD→XAF fallback rate = %v, expected ~609", rate)
	}
}

func TestFallbackRate_AllSupportedPairs(t *testing.T) {
	currencies := []string{"USD", "EUR", "GBP", "XAF", "XOF"}
	for _, from := range currencies {
		for _, to := range currencies {
			if from == to {
				continue
			}
			t.Run(fmt.Sprintf("%s→%s", from, to), func(t *testing.T) {
				rate := fallbackRate(from, to)
				if rate <= 0 {
					t.Errorf("fallbackRate(%q, %q) = %v, want > 0", from, to, rate)
				}
			})
		}
	}
}

func TestFallbackRate_UnknownCurrency(t *testing.T) {
	// Unknown currency → zero rate from defaultFXRates, so toUSD = 0 → returns 0.
	rate := fallbackRate("USD", "XXX")
	if rate != 0 {
		t.Errorf("fallbackRate(USD, XXX) = %v, want 0", rate)
	}
}

// ── roundTo ───────────────────────────────────────────────────────────────────

func TestRoundTo(t *testing.T) {
	cases := []struct {
		v, want  float64
		places   int
	}{
		{1.23456789, 1.234568, 6},
		{1.23456789, 1.2346, 4},
		{1.23456789, 1.23, 2},
		{1.23456789, 1.2, 1},
		{1.23456789, 1.0, 0},
		{0.925926, 0.926, 3},
		{0.925926, 0.93, 2},
		{100.0, 100.0, 2},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("round(%.8f,%d)", tc.v, tc.places), func(t *testing.T) {
			got := roundTo(tc.v, tc.places)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("roundTo(%v, %d) = %v, want %v", tc.v, tc.places, got, tc.want)
			}
		})
	}
}

// ── groupExchangeByPeriod ─────────────────────────────────────────────────────

func TestGroupExchangeByPeriod_Empty(t *testing.T) {
	groups := groupExchangeByPeriod(nil)
	if len(groups) != 0 {
		t.Errorf("empty input: expected 0 groups, got %d", len(groups))
	}
}

func TestGroupExchangeByPeriod_OrderIsThisWeekFirstEarlierLast(t *testing.T) {
	items := []ExchangeHistoryItem{
		{ID: "e", Period: "earlier"},
		{ID: "l", Period: "last_week"},
		{ID: "t", Period: "this_week"},
	}
	groups := groupExchangeByPeriod(items)
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	wantOrder := []string{"this_week", "last_week", "earlier"}
	for i, want := range wantOrder {
		if groups[i].Period != want {
			t.Errorf("groups[%d].Period = %q, want %q", i, groups[i].Period, want)
		}
	}
}

func TestGroupExchangeByPeriod_EmptyPeriodsSkipped(t *testing.T) {
	// Only "this_week" items — "last_week" and "earlier" buckets should be absent.
	items := []ExchangeHistoryItem{
		{ID: "1", Period: "this_week"},
		{ID: "2", Period: "this_week"},
	}
	groups := groupExchangeByPeriod(items)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Period != "this_week" {
		t.Errorf("group period: got %q, want this_week", groups[0].Period)
	}
	if groups[0].Count != 2 {
		t.Errorf("group count: got %d, want 2", groups[0].Count)
	}
	if groups[0].Label != "THIS WEEK" {
		t.Errorf("group label: got %q, want THIS WEEK", groups[0].Label)
	}
}

func TestGroupExchangeByPeriod_LabelsCorrect(t *testing.T) {
	items := []ExchangeHistoryItem{
		{ID: "a", Period: "this_week"},
		{ID: "b", Period: "last_week"},
		{ID: "c", Period: "earlier"},
	}
	groups := groupExchangeByPeriod(items)
	labelMap := map[string]string{
		"this_week": "THIS WEEK",
		"last_week": "LAST WEEK",
		"earlier":   "EARLIER",
	}
	for _, g := range groups {
		if want := labelMap[g.Period]; g.Label != want {
			t.Errorf("period %q: label=%q, want %q", g.Period, g.Label, want)
		}
	}
}

// ── mapExchangeTx ─────────────────────────────────────────────────────────────

func TestMapExchangeTx_MetadataParsed(t *testing.T) {
	meta, _ := json.Marshal(map[string]any{
		"from_currency": "EUR",
		"to_currency":   "USD",
		"from_amount":   1000.0,
		"to_amount":     1080.0,
		"rate":          1.08,
		"fee":           0,
	})
	txID := uuid.New()
	now := time.Now()
	tx := db.Transaction{
		ID:        txID,
		Reference: "EXC-ABCDEF12",
		Type:      "exchange",
		Status:    "completed",
		Metadata:  meta,
		CreatedAt: now,
	}

	item := mapExchangeTx(tx)

	if item.From != "EUR" {
		t.Errorf("From: got %q, want EUR", item.From)
	}
	if item.To != "USD" {
		t.Errorf("To: got %q, want USD", item.To)
	}
	if item.FromAmount != 1000.0 {
		t.Errorf("FromAmount: got %v, want 1000", item.FromAmount)
	}
	if item.ToAmount != 1080.0 {
		t.Errorf("ToAmount: got %v, want 1080", item.ToAmount)
	}
	if item.Rate != 1.08 {
		t.Errorf("Rate: got %v, want 1.08", item.Rate)
	}
	if item.Status != "completed" {
		t.Errorf("Status: got %q, want completed", item.Status)
	}
	if item.Reference != "EXC-ABCDEF12" {
		t.Errorf("Reference: got %q, want EXC-ABCDEF12", item.Reference)
	}
	if item.ID != txID.String() {
		t.Errorf("ID: got %q, want %q", item.ID, txID.String())
	}
}

func TestMapExchangeTx_RateLabel(t *testing.T) {
	meta, _ := json.Marshal(map[string]any{
		"from_currency": "USD",
		"to_currency":   "XAF",
		"from_amount":   100.0,
		"to_amount":     65500.0,
		"rate":          655.0,
	})
	tx := db.Transaction{
		ID:       uuid.New(),
		Metadata: meta,
		Status:   "completed",
	}
	item := mapExchangeTx(tx)
	if item.RateLabel != "1 USD = 655 XAF" {
		t.Errorf("RateLabel: got %q, want '1 USD = 655 XAF'", item.RateLabel)
	}
}

func TestMapExchangeTx_FromToFormatted(t *testing.T) {
	meta, _ := json.Marshal(map[string]any{
		"from_currency": "EUR",
		"to_currency":   "USD",
		"from_amount":   1000.0,
		"to_amount":     1080.0,
		"rate":          1.08,
	})
	tx := db.Transaction{ID: uuid.New(), Metadata: meta, Status: "completed"}
	item := mapExchangeTx(tx)

	if item.FromFormatted == "" {
		t.Error("FromFormatted is empty")
	}
	if item.ToFormatted == "" {
		t.Error("ToFormatted is empty")
	}
}

func TestMapExchangeTx_EmptyMetadata(t *testing.T) {
	// Should not panic; just produce zero/empty fields.
	tx := db.Transaction{ID: uuid.New(), Metadata: json.RawMessage(`{}`), Status: "pending"}
	item := mapExchangeTx(tx)
	if item.Status != "pending" {
		t.Errorf("Status: got %q, want pending", item.Status)
	}
	if item.From != "" {
		t.Errorf("From should be empty for empty metadata, got %q", item.From)
	}
}

// ── ExchangeService sentinel errors ──────────────────────────────────────────

func TestExchangeSentinelErrors(t *testing.T) {
	// Sentinel errors must be non-nil and have distinct messages.
	errors := []error{ErrSameCurrency, ErrUnsupportedPair, ErrQuoteExpired}
	for i, e := range errors {
		if e == nil {
			t.Errorf("sentinel[%d] is nil", i)
		}
	}
	if ErrSameCurrency == ErrUnsupportedPair {
		t.Error("ErrSameCurrency and ErrUnsupportedPair must be distinct")
	}
	if ErrUnsupportedPair == ErrQuoteExpired {
		t.Error("ErrUnsupportedPair and ErrQuoteExpired must be distinct")
	}
}

// ── ExchangeResult struct completeness ────────────────────────────────────────

func TestExchangeResultJSON(t *testing.T) {
	result := ExchangeResult{
		ID:         "uuid-1",
		Reference:  "EXC-ABCDE123",
		From:       "USD",
		To:         "EUR",
		Rate:       0.925926,
		FromAmount: 1000,
		ToAmount:   925.93,
		Fee:        0,
		Status:     "completed",
		CreatedAt:  time.Now(),
	}
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal(ExchangeResult): %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	requiredKeys := []string{"id", "reference", "from", "to", "rate", "from_amount", "to_amount", "fee", "status", "created_at"}
	for _, k := range requiredKeys {
		if _, ok := out[k]; !ok {
			t.Errorf("JSON missing key %q", k)
		}
	}
}

// ── InsightsSummary / InsightsData struct completeness ────────────────────────

func TestInsightsSummaryJSON(t *testing.T) {
	summary := InsightsSummary{
		TotalBalance:      119_476,
		TotalFormatted:    "$119,476",
		IncomeMonth:       16_134,
		IncomeFormatted:   "$16,134",
		SpendingMonth:     2_654,
		SpendingFormatted: "$2,654",
		NetFlow:           13_480,
		NetFormatted:      "+$13,480",
	}
	b, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("json.Marshal(InsightsSummary): %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	keys := []string{
		"total_balance", "total_formatted",
		"income_month", "income_formatted",
		"spending_month", "spending_formatted",
		"net_flow", "net_formatted",
	}
	for _, k := range keys {
		if _, ok := out[k]; !ok {
			t.Errorf("InsightsSummary JSON missing key %q", k)
		}
	}
}

func TestInsightsDataJSON(t *testing.T) {
	data := InsightsData{
		Period:                 "1m",
		Summary:                InsightsSummary{},
		BalanceTrends:          []BalanceTrendPoint{},
		AssetAllocation:        AssetAllocationInsight{},
		MonthlyFlow:            []MonthlyFlowPoint{},
		SpendingByType:         []SpendingByTypePoint{},
		TotalActivityFormatted: "$0",
	}
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal(InsightsData): %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	keys := []string{
		"period", "summary", "fiat_balance", "crypto_balance",
		"trend_change", "trend_formatted", "balance_trends",
		"asset_allocation", "monthly_flow", "net_flow", "net_formatted",
		"spending_by_type", "total_activity", "total_activity_formatted",
	}
	for _, k := range keys {
		if _, ok := out[k]; !ok {
			t.Errorf("InsightsData JSON missing key %q", k)
		}
	}
}
