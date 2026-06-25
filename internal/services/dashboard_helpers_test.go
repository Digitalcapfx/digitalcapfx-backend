package services

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// ─── pgNumericToFloat ─────────────────────────────────────────────────────────

func TestPgNumericToFloat(t *testing.T) {
	cases := []struct {
		name string
		in   pgtype.Numeric
		want float64
	}{
		{
			name: "zero value",
			in:   pgtype.Numeric{},
			want: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pgNumericToFloat(c.in)
			if got != c.want {
				t.Errorf("pgNumericToFloat(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// ─── parseFloatSafe ───────────────────────────────────────────────────────────

func TestParseFloatSafe(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"0", 0},
		{"100.50", 100.50},
		{"15000.00", 15000.00},
		{"", 0},
		{"abc", 0},
		{"-1.5", -1.5},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := parseFloatSafe(c.in)
			if got != c.want {
				t.Errorf("parseFloatSafe(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// ─── roundUSD ─────────────────────────────────────────────────────────────────

func TestRoundUSD(t *testing.T) {
	cases := []struct {
		in   float64
		want float64
	}{
		{0, 0},
		{1.006, 1.01},   // 1.005 is a known IEEE 754 hazard; 1.006 rounds cleanly
		{1.004, 1.00},
		{12450.757, 12450.76},
		{100.0, 100.0},
	}
	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			got := roundUSD(c.in)
			if got != c.want {
				t.Errorf("roundUSD(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// ─── initials ─────────────────────────────────────────────────────────────────

func TestInitials(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Alice Dupont", "AD"},
		{"Bob", "B"},
		{"", ""},
		{"Jean-Pierre Kouassi", "JK"},
		{"aminata sow", "AS"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := initials(c.in)
			if got != c.want {
				t.Errorf("initials(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
