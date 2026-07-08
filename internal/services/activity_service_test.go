package services

import (
	"fmt"
	"testing"
	"time"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

// ── resolveType ───────────────────────────────────────────────────────────────

func TestResolveType_ReceivedFiat(t *testing.T) {
	cases := []string{"credit", "transfer_in"}
	for _, typ := range cases {
		t.Run(typ, func(t *testing.T) {
			item := db.ActivityItem{Source: "fiat", Type: typ, Asset: "USD"}
			uiType, title, icon := resolveType(item)
			if uiType != "received" {
				t.Errorf("uiType: want received, got %s", uiType)
			}
			if title != "Received USD" {
				t.Errorf("title: want 'Received USD', got %s", title)
			}
			if icon != "received_fiat" {
				t.Errorf("icon: want received_fiat, got %s", icon)
			}
		})
	}
}

func TestResolveType_ReceivedCrypto(t *testing.T) {
	cases := []struct{ source, assetLabel string }{
		{"crypto", "BTC"},
		{"caas", "USDC"},
	}
	for _, c := range cases {
		t.Run(c.source+"/"+c.assetLabel, func(t *testing.T) {
			item := db.ActivityItem{Source: c.source, Type: "credit", Asset: c.assetLabel}
			_, _, icon := resolveType(item)
			if icon != "received_crypto" {
				t.Errorf("icon: want received_crypto, got %s", icon)
			}
		})
	}
}

func TestResolveType_SentFiat(t *testing.T) {
	cases := []string{"debit", "transfer_out"}
	for _, typ := range cases {
		t.Run(typ, func(t *testing.T) {
			item := db.ActivityItem{Source: "fiat", Type: typ, Asset: "EUR"}
			uiType, title, icon := resolveType(item)
			if uiType != "sent" {
				t.Errorf("uiType: want sent, got %s", uiType)
			}
			if title != "Sent EUR" {
				t.Errorf("title: want 'Sent EUR', got %s", title)
			}
			if icon != "sent_fiat" {
				t.Errorf("icon: want sent_fiat, got %s", icon)
			}
		})
	}
}

func TestResolveType_SentCrypto(t *testing.T) {
	item := db.ActivityItem{Source: "crypto", Type: "debit", Asset: "eth"}
	uiType, title, icon := resolveType(item)
	if uiType != "sent" {
		t.Errorf("uiType: want sent, got %s", uiType)
	}
	if title != "Sent ETH" {
		t.Errorf("title: want 'Sent ETH', got %s", title)
	}
	if icon != "sent_crypto" {
		t.Errorf("icon: want sent_crypto, got %s", icon)
	}
}

func TestResolveType_ExchangeWithDescription(t *testing.T) {
	item := db.ActivityItem{Source: "fiat", Type: "exchange", Asset: "EUR", Description: "EUR → USD"}
	uiType, title, icon := resolveType(item)
	if uiType != "exchanged" {
		t.Errorf("uiType: want exchanged, got %s", uiType)
	}
	if title != "Exchanged EUR → USD" {
		t.Errorf("title: want 'Exchanged EUR → USD', got %q", title)
	}
	if icon != "exchanged" {
		t.Errorf("icon: want exchanged, got %s", icon)
	}
}

func TestResolveType_ExchangeNoDescription(t *testing.T) {
	item := db.ActivityItem{Source: "fiat", Type: "exchange", Asset: "GBP"}
	_, title, _ := resolveType(item)
	if title != "Exchanged GBP" {
		t.Errorf("title: want 'Exchanged GBP', got %s", title)
	}
}

func TestResolveType_Deposit(t *testing.T) {
	item := db.ActivityItem{Source: "fiat", Type: "deposit", Asset: "USD"}
	uiType, title, icon := resolveType(item)
	if uiType != "deposited" {
		t.Errorf("uiType: want deposited, got %s", uiType)
	}
	if title != "Deposited USD" {
		t.Errorf("title: want 'Deposited USD', got %s", title)
	}
	if icon != "deposited" {
		t.Errorf("icon: want deposited, got %s", icon)
	}
}

func TestResolveType_Withdrawal(t *testing.T) {
	item := db.ActivityItem{Source: "fiat", Type: "withdrawal", Asset: "XAF"}
	uiType, title, icon := resolveType(item)
	if uiType != "withdrawn" {
		t.Errorf("uiType: want withdrawn, got %s", uiType)
	}
	if title != "Withdrawn XAF" {
		t.Errorf("title: want 'Withdrawn XAF', got %s", title)
	}
	if icon != "withdrawn" {
		t.Errorf("icon: want withdrawn, got %s", icon)
	}
}

func TestResolveType_Unknown(t *testing.T) {
	item := db.ActivityItem{Source: "fiat", Type: "unknown_type", Asset: "USD", Description: "Some payment"}
	uiType, title, icon := resolveType(item)
	if uiType != "unknown_type" {
		t.Errorf("uiType: want 'unknown_type', got %s", uiType)
	}
	if title != "Some payment" {
		t.Errorf("title: want 'Some payment', got %s", title)
	}
	if icon != "other" {
		t.Errorf("icon: want other, got %s", icon)
	}
}

// ── formatActivityAmount ──────────────────────────────────────────────────────

func TestFormatActivityAmount(t *testing.T) {
	cases := []struct {
		source, assetType, amount, sign, want string
	}{
		// Bitcoin — formatCrypto already appends " BTC", so sign+formatCrypto = "+0.1250 BTC"
		{"crypto", "BTC", "0.125", "+", "+0.1250 BTC"},
		{"crypto", "BTC", "1.5", "-", "-1.5000 BTC"},
		// ETH — 3dp suffix already in formatCrypto output
		{"crypto", "ETH", "0.75", "+", "+0.750 ETH"},
		// SOL — 2dp suffix already in formatCrypto output
		{"crypto", "SOL", "10", "+", "+10.00 SOL"},
		// USDC/USDT — 2dp + symbol suffix
		{"caas", "USDC", "500", "+", "+500.00 USDC"},
		{"caas", "USDT", "1234.5", "-", "-1,234.50 USDT"},
		// USD — $ prefix
		{"fiat", "USD", "1000", "-", "-$1,000.00"},
		// EUR — € prefix
		{"fiat", "EUR", "850.50", "-", "-€850.50"},
		// GBP — £ prefix
		{"fiat", "GBP", "200", "+", "+£200.00"},
		// XAF — 0dp + code suffix
		{"fiat", "XAF", "150000", "+", "+150,000 XAF"},
		// XOF — 0dp + code suffix
		{"fiat", "XOF", "75000", "-", "-75,000 XOF"},
		// unknown currency — 2dp + code
		{"fiat", "CAD", "99.9", "+", "+99.90 CAD"},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%s", tc.assetType, tc.amount), func(t *testing.T) {
			item := db.ActivityItem{Source: tc.source, Asset: tc.assetType, Amount: tc.amount, AmountSign: tc.sign}
			got := formatActivityAmount(item)
			if got != tc.want {
				t.Errorf("formatActivityAmount(%q, %q, %q) = %q, want %q",
					tc.assetType, tc.amount, tc.sign, got, tc.want)
			}
		})
	}
}

// ── buildSubtitle ─────────────────────────────────────────────────────────────

func TestBuildSubtitle_CounterName(t *testing.T) {
	ts, _ := time.Parse("15:04", "20:31")
	item := db.ActivityItem{
		CreatedAt:   time.Date(2026, 6, 23, 20, 31, 0, 0, time.UTC),
		CounterName: "Payment from client",
		Type:        "credit",
	}
	got := buildSubtitle(item)
	want := "8:31 PM · Payment from client"
	if got != want {
		t.Errorf("buildSubtitle with counter_name: got %q, want %q (ts=%v)", got, want, ts)
	}
}

func TestBuildSubtitle_DescriptionFallback(t *testing.T) {
	item := db.ActivityItem{
		CreatedAt:   time.Date(2026, 6, 23, 8, 5, 0, 0, time.UTC),
		Description: "Monthly subscription",
		Type:        "debit",
	}
	got := buildSubtitle(item)
	want := "8:05 AM · Monthly subscription"
	if got != want {
		t.Errorf("buildSubtitle with description: got %q, want %q", got, want)
	}
}

func TestBuildSubtitle_Exchange_NoDescription(t *testing.T) {
	// For exchange type, description is the "EUR → USD" pair used in the title.
	// Subtitle should be just the time.
	item := db.ActivityItem{
		CreatedAt:   time.Date(2026, 6, 23, 14, 0, 0, 0, time.UTC),
		Description: "EUR → USD",
		Type:        "exchange",
	}
	got := buildSubtitle(item)
	want := "2:00 PM"
	if got != want {
		t.Errorf("buildSubtitle exchange: got %q, want %q", got, want)
	}
}

func TestBuildSubtitle_TimeOnly(t *testing.T) {
	item := db.ActivityItem{
		CreatedAt: time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC),
		Type:      "credit",
	}
	got := buildSubtitle(item)
	want := "12:00 AM"
	if got != want {
		t.Errorf("buildSubtitle time-only: got %q, want %q", got, want)
	}
}

func TestBuildSubtitle_CounterNameOverridesDescription(t *testing.T) {
	// When both are set, counter_name wins.
	item := db.ActivityItem{
		CreatedAt:   time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC),
		CounterName: "Alice Dupont",
		Description: "Should not appear",
		Type:        "credit",
	}
	got := buildSubtitle(item)
	want := "9:00 AM · Alice Dupont"
	if got != want {
		t.Errorf("counter_name should override description: got %q, want %q", got, want)
	}
}

// ── dayLabel ──────────────────────────────────────────────────────────────────

func TestDayLabel_Today(t *testing.T) {
	now := time.Now()
	label := dayLabel(now)
	if label != "Today" {
		t.Errorf("dayLabel(today): got %q, want Today", label)
	}
}

func TestDayLabel_Yesterday(t *testing.T) {
	yesterday := time.Now().AddDate(0, 0, -1)
	label := dayLabel(yesterday)
	if label != "Yesterday" {
		t.Errorf("dayLabel(yesterday): got %q, want Yesterday", label)
	}
}

func TestDayLabel_ThisWeek(t *testing.T) {
	// 3 days ago is within 7 days → should return weekday name.
	threeDaysAgo := time.Now().AddDate(0, 0, -3)
	label := dayLabel(threeDaysAgo)
	want := threeDaysAgo.Format("Monday")
	if label != want {
		t.Errorf("dayLabel(3d ago): got %q, want %q", label, want)
	}
}

func TestDayLabel_Older(t *testing.T) {
	// 10 days ago → "Mon 2 Jan" style.
	tenDaysAgo := time.Now().AddDate(0, 0, -10)
	label := dayLabel(tenDaysAgo)
	want := tenDaysAgo.Format("Mon 2 Jan")
	if label != want {
		t.Errorf("dayLabel(10d ago): got %q, want %q", label, want)
	}
}

// ── groupByCalendarDay ────────────────────────────────────────────────────────

func makeEntry(id string, ts time.Time, typ string) ActivityEntry {
	return ActivityEntry{
		ID:        id,
		Type:      typ,
		CreatedAt: ts,
	}
}

func TestGroupByCalendarDay_Empty(t *testing.T) {
	groups := groupByCalendarDay(nil)
	if len(groups) != 0 {
		t.Errorf("empty input: expected 0 groups, got %d", len(groups))
	}
}

// middayToday returns today at 12:00 local time so hour offsets in these
// tests never cross a calendar-day boundary, and dayLabel's internal
// time.Now() comparisons agree regardless of when the tests run.
func middayToday() time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 12, 0, 0, 0, time.Local)
}

func TestGroupByCalendarDay_SingleDay(t *testing.T) {
	now := middayToday()
	entries := []ActivityEntry{
		makeEntry("1", now, "sent"),
		makeEntry("2", now.Add(-time.Hour), "received"),
		makeEntry("3", now.Add(-2*time.Hour), "deposited"),
	}
	groups := groupByCalendarDay(entries)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].DayLabel != "Today" {
		t.Errorf("day label: got %q, want Today", groups[0].DayLabel)
	}
	if groups[0].Count != 3 {
		t.Errorf("count: got %d, want 3", groups[0].Count)
	}
}

func TestGroupByCalendarDay_MultipleDays(t *testing.T) {
	now := middayToday()
	yesterday := now.AddDate(0, 0, -1)
	twoDaysAgo := now.AddDate(0, 0, -2)

	entries := []ActivityEntry{
		makeEntry("1", now, "sent"),
		makeEntry("2", yesterday, "received"),
		makeEntry("3", yesterday.Add(-30*time.Minute), "received"),
		makeEntry("4", twoDaysAgo, "deposited"),
	}
	groups := groupByCalendarDay(entries)

	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	if groups[0].DayLabel != "Today" {
		t.Errorf("group[0] label: got %q, want Today", groups[0].DayLabel)
	}
	if groups[0].Count != 1 {
		t.Errorf("group[0] count: got %d, want 1", groups[0].Count)
	}
	if groups[1].DayLabel != "Yesterday" {
		t.Errorf("group[1] label: got %q, want Yesterday", groups[1].DayLabel)
	}
	if groups[1].Count != 2 {
		t.Errorf("group[1] count: got %d, want 2", groups[1].Count)
	}
}

func TestGroupByCalendarDay_PreservesOrder(t *testing.T) {
	// Entries arrive newest-first (as DB would return them).
	now := time.Now()
	entries := []ActivityEntry{
		makeEntry("new", now, "sent"),
		makeEntry("old", now.AddDate(0, 0, -5), "received"),
	}
	groups := groupByCalendarDay(entries)
	if groups[0].Items[0].ID != "new" {
		t.Errorf("first group should contain newest entry")
	}
	if groups[1].Items[0].ID != "old" {
		t.Errorf("second group should contain older entry")
	}
}

// ── enrichActivity ────────────────────────────────────────────────────────────

func TestEnrichActivity_SetsAllFields(t *testing.T) {
	ts := time.Date(2026, 6, 23, 10, 30, 0, 0, time.UTC)
	item := db.ActivityItem{
		ID:          "tx-001",
		Source:      "fiat",
		Type:        "credit",
		Asset:       "USD",
		Amount:      "500.00",
		AmountSign:  "+",
		Status:      "completed",
		CounterName: "Alice",
		CreatedAt:   ts,
	}
	entry := enrichActivity(item)

	if entry.ID != "tx-001" {
		t.Errorf("ID: got %q", entry.ID)
	}
	if entry.Type != "received" {
		t.Errorf("Type: got %q, want received", entry.Type)
	}
	if entry.Title != "Received USD" {
		t.Errorf("Title: got %q", entry.Title)
	}
	if entry.AmountFormatted != "+$500.00" {
		t.Errorf("AmountFormatted: got %q, want +$500.00", entry.AmountFormatted)
	}
	if entry.Subtitle != "10:30 AM · Alice" {
		t.Errorf("Subtitle: got %q", entry.Subtitle)
	}
	if entry.IconType != "received_fiat" {
		t.Errorf("IconType: got %q, want received_fiat", entry.IconType)
	}
	if entry.Status != "completed" {
		t.Errorf("Status: got %q", entry.Status)
	}
}
