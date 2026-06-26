package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

// ─── Response types ───────────────────────────────────────────────────────────

// ActivityEntry enriches db.ActivityItem with display-ready fields.
type ActivityEntry struct {
	ID              string    `json:"id"`
	Source          string    `json:"source"`           // "fiat" | "crypto" | "caas"
	Type            string    `json:"type"`             // "sent" | "received" | "exchanged" | "deposited" | "withdrawn"
	Title           string    `json:"title"`            // "Received BTC"
	Subtitle        string    `json:"subtitle"`         // "8:31 PM · Payment from client"
	Asset           string    `json:"asset"`            // "BTC"
	AmountFormatted string    `json:"amount_formatted"` // "+0.125 BTC" | "-$500.00"
	AmountSign      string    `json:"amount_sign"`      // "+" | "-"
	Status          string    `json:"status"`           // "completed" | "pending" | "failed"
	IconType        string    `json:"icon_type"`        // "received_crypto" | "sent_fiat" | "exchanged" | "deposited" | "withdrawn"
	CreatedAt       time.Time `json:"created_at"`
}

// ActivityDayGroup groups entries under a calendar day header.
type ActivityDayGroup struct {
	DayLabel string          `json:"day_label"` // "Monday" | "Tuesday" | ...
	Date     string          `json:"date"`       // "2026-06-23"
	Items    []ActivityEntry `json:"items"`
	Count    int             `json:"count"`
}

// ActivityFeed is the full activity screen payload.
type ActivityFeed struct {
	Groups []ActivityDayGroup `json:"groups"`
	Total  int64              `json:"total"`
	Page   int32              `json:"page"`
	Limit  int32              `json:"limit"`
}

// ─── Service ──────────────────────────────────────────────────────────────────

type ActivityService struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

func NewActivityService(pool *pgxpool.Pool, logger *zap.Logger) *ActivityService {
	return &ActivityService{pool: pool, logger: logger}
}

// GetFeed returns the paginated, filtered, day-grouped activity feed.
// typeFilter: "" | "sent" | "received" | "exchanged" | "deposited" | "withdrawn"
func (s *ActivityService) GetFeed(
	ctx context.Context,
	userID uuid.UUID,
	typeFilter, search string,
	page, limit int32,
) (*ActivityFeed, error) {
	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	q := db.New(s.pool)

	rawItems, err := q.ListActivity(ctx, db.ListActivityParams{
		UserID:     userID,
		TypeFilter: typeFilter,
		Search:     search,
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		s.logger.Warn("activity feed: db query failed, returning empty", zap.Error(err))
		rawItems = nil
	}

	total, _ := q.CountActivity(ctx, db.ListActivityParams{
		UserID:     userID,
		TypeFilter: typeFilter,
		Search:     search,
	})

	entries := make([]ActivityEntry, 0, len(rawItems))
	for _, item := range rawItems {
		entries = append(entries, enrichActivity(item))
	}

	groups := groupByCalendarDay(entries)

	return &ActivityFeed{
		Groups: groups,
		Total:  total,
		Page:   page,
		Limit:  limit,
	}, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// enrichActivity converts a raw db.ActivityItem into a display-ready ActivityEntry.
func enrichActivity(item db.ActivityItem) ActivityEntry {
	uiType, title, iconType := resolveType(item)
	amountFmt := formatActivityAmount(item)
	subtitle := buildSubtitle(item)

	return ActivityEntry{
		ID:              item.ID,
		Source:          item.Source,
		Type:            uiType,
		Title:           title,
		Subtitle:        subtitle,
		Asset:           item.Asset,
		AmountFormatted: amountFmt,
		AmountSign:      item.AmountSign,
		Status:          item.Status,
		IconType:        iconType,
		CreatedAt:       item.CreatedAt,
	}
}

// resolveType maps the raw (source, type) pair to (ui_type, display_title, icon_type).
func resolveType(item db.ActivityItem) (uiType, title, iconType string) {
	asset := strings.ToUpper(item.Asset)
	isCrypto := item.Source == "crypto" || item.Source == "caas"

	switch item.Type {
	case "credit", "transfer_in":
		uiType = "received"
		title = fmt.Sprintf("Received %s", asset)
		if isCrypto {
			iconType = "received_crypto"
		} else {
			iconType = "received_fiat"
		}
	case "debit", "transfer_out":
		uiType = "sent"
		title = fmt.Sprintf("Sent %s", asset)
		if isCrypto {
			iconType = "sent_crypto"
		} else {
			iconType = "sent_fiat"
		}
	case "exchange":
		uiType = "exchanged"
		// Description carries "EUR → USD" for exchange items.
		if item.Description != "" {
			title = fmt.Sprintf("Exchanged %s", item.Description)
		} else {
			title = fmt.Sprintf("Exchanged %s", asset)
		}
		iconType = "exchanged"
	case "deposit":
		uiType = "deposited"
		title = fmt.Sprintf("Deposited %s", asset)
		iconType = "deposited"
	case "withdrawal":
		uiType = "withdrawn"
		title = fmt.Sprintf("Withdrawn %s", asset)
		iconType = "withdrawn"
	default:
		uiType = item.Type
		title = item.Description
		iconType = "other"
	}
	return
}

// formatActivityAmount returns "+0.125 BTC", "-$500.00", "+5,000.00 USDT" etc.
func formatActivityAmount(item db.ActivityItem) string {
	sign := item.AmountSign
	amount := parseFloatSafe(item.Amount)
	asset := strings.ToUpper(item.Asset)

	switch asset {
	case "BTC", "ETH", "SOL", "LTC", "TRX", "POL", "BCH", "XRP":
		return sign + formatCrypto(amount, asset)
	case "USDC", "USDT":
		return fmt.Sprintf("%s%s %s", sign, formatNumber(amount, 2), asset)
	case "USD":
		return fmt.Sprintf("%s$%s", sign, formatNumber(amount, 2))
	case "EUR":
		return fmt.Sprintf("%s€%s", sign, formatNumber(amount, 2))
	case "GBP":
		return fmt.Sprintf("%s£%s", sign, formatNumber(amount, 2))
	case "XAF", "XOF":
		return fmt.Sprintf("%s%s %s", sign, formatNumber(amount, 0), asset)
	default:
		return fmt.Sprintf("%s%s %s", sign, formatNumber(amount, 2), asset)
	}
}

// buildSubtitle constructs "8:31 PM · Payment from client" or just "8:31 PM".
func buildSubtitle(item db.ActivityItem) string {
	timeStr := item.CreatedAt.Format("3:04 PM")
	if item.CounterName != "" {
		return timeStr + " · " + item.CounterName
	}
	if item.Description != "" && item.Type != "exchange" {
		return timeStr + " · " + item.Description
	}
	return timeStr
}

// groupByCalendarDay groups ActivityEntries into day buckets (Monday, Tuesday, …).
func groupByCalendarDay(entries []ActivityEntry) []ActivityDayGroup {
	type bucket struct {
		label string
		date  string
		items []ActivityEntry
	}

	var ordered []string
	buckets := map[string]*bucket{}

	for _, e := range entries {
		key := e.CreatedAt.Format("2006-01-02")
		if _, ok := buckets[key]; !ok {
			label := dayLabel(e.CreatedAt)
			buckets[key] = &bucket{label: label, date: key}
			ordered = append(ordered, key)
		}
		buckets[key].items = append(buckets[key].items, e)
	}

	groups := make([]ActivityDayGroup, 0, len(ordered))
	for _, key := range ordered {
		b := buckets[key]
		groups = append(groups, ActivityDayGroup{
			DayLabel: b.label,
			Date:     b.date,
			Items:    b.items,
			Count:    len(b.items),
		})
	}
	return groups
}

// dayLabel returns a human-friendly day label relative to today.
func dayLabel(t time.Time) string {
	now := time.Now()
	todayDate := now.Format("2006-01-02")
	yesterdayDate := now.AddDate(0, 0, -1).Format("2006-01-02")
	tDate := t.Format("2006-01-02")

	switch tDate {
	case todayDate:
		return "Today"
	case yesterdayDate:
		return "Yesterday"
	default:
		// Within the last 7 days → show weekday name.
		if now.Sub(t) < 7*24*time.Hour {
			return t.Format("Monday")
		}
		// Older → show "Mon 23 Jun"
		return t.Format("Mon 2 Jan")
	}
}
