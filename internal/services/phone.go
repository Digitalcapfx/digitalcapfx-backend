package services

import "strings"

// ─── Phone number canonicalisation ─────────────────────────────────────────────
//
// Goal: no matter how a phone number is typed (spaces, dashes, parentheses, a
// leading "+", "00" international prefix, or a national "0" trunk prefix), the
// same subscriber always resolves to the same account.
//
// Strategy:
//   • On WRITE (register), store the canonical E.164 form ("+" + country + number).
//   • On LOOKUP (login, forgot/reset PIN, crypto send, webhooks), match against
//     every equivalent form, so even legacy rows stored in a different format
//     still resolve. This needs no risky data migration.
//
// E.164 inputs (with "+" or "00") are country-independent and always canonicalise
// exactly. National ("0…") numbers are expanded using defaultCallingCode.

// defaultCallingCode is the numeric country calling code (e.g. "234") used to
// expand national, 0-prefixed numbers into E.164. Set once at startup from
// config (DEFAULT_COUNTRY_CODE). Empty = leave national numbers untouched.
var defaultCallingCode string

// SetDefaultCallingCode configures the calling code used for national numbers.
// Accepts "234", "+234" or "00234" — stored as bare digits.
func SetDefaultCallingCode(cc string) {
	cc = strings.TrimSpace(cc)
	cc = strings.TrimPrefix(cc, "+")
	cc = strings.TrimPrefix(cc, "00")
	defaultCallingCode = cc
}

// stripPhone reduces a raw string to digits plus whether it had a leading "+".
func stripPhone(raw string) (digits string, hasPlus bool) {
	raw = strings.TrimSpace(raw)
	var b strings.Builder
	for i, r := range raw {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0:
			hasPlus = true
		}
	}
	return b.String(), hasPlus
}

// canonicalPhone converts any reasonably-formatted input into a single canonical
// E.164 string. Never errors — unparseable input is returned best-effort so a
// bad value can still be looked up (and simply won't match).
func canonicalPhone(raw string) string {
	d, hasPlus := stripPhone(raw)
	if d == "" {
		return strings.TrimSpace(raw)
	}
	switch {
	case hasPlus:
		return "+" + d
	case strings.HasPrefix(d, "00"):
		return "+" + strings.TrimPrefix(d, "00")
	case strings.HasPrefix(d, "0"):
		if defaultCallingCode != "" {
			return "+" + defaultCallingCode + strings.TrimPrefix(d, "0")
		}
		return d // national number, no default country → leave as-is
	default:
		// Bare digits with no "+" and no leading "0": assume the country code is
		// already present (e.g. "2348012345678").
		return "+" + d
	}
}

// normalizePhone is the canonical form stored on write.
func normalizePhone(raw string) string { return canonicalPhone(raw) }

// phoneCandidates returns every equivalent representation of a number, so a
// lookup matches regardless of how the stored value was formatted (canonical
// E.164, "+"-less, national 0-form, or the raw input itself).
func phoneCandidates(raw string) []string {
	seen := map[string]bool{}
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			seen[s] = true
		}
	}

	add(raw)
	digits, _ := stripPhone(raw)
	add(digits)

	e164 := canonicalPhone(raw)
	add(e164)
	if strings.HasPrefix(e164, "+") {
		noPlus := e164[1:]
		add(noPlus)
		if defaultCallingCode != "" && strings.HasPrefix(noPlus, defaultCallingCode) {
			national := strings.TrimPrefix(noPlus, defaultCallingCode)
			add("0" + national) // national 0-form
			add(national)       // bare national digits
		}
	}

	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	return out
}
