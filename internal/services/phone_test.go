package services

import "testing"

// Every reasonable way of typing the same Nigerian number must canonicalise to
// one E.164 value, so a user always resolves to the same account.
func TestCanonicalPhone_AllFormsCollapse(t *testing.T) {
	SetDefaultCallingCode("234")
	defer SetDefaultCallingCode("")

	const want = "+2348086604812"
	inputs := []string{
		"+2348086604812",
		"2348086604812",
		"08086604812",
		"+234 808 660 4812",
		"234-808-660-4812",
		"(0808) 660-4812",
		"002348086604812",
		"  +234 (808) 660-4812  ",
	}
	for _, in := range inputs {
		if got := canonicalPhone(in); got != want {
			t.Errorf("canonicalPhone(%q) = %q, want %q", in, got, want)
		}
	}
}

// A lookup for any form must produce candidates that include the forms a legacy
// row might have been stored as (E.164, national 0-form, bare, +-less).
func TestPhoneCandidates_CrossFormMatch(t *testing.T) {
	SetDefaultCallingCode("234")
	defer SetDefaultCallingCode("")

	// Whatever the user types, the candidate set must contain both the canonical
	// E.164 and the national 0-form, so it matches a row stored either way.
	for _, in := range []string{"+2348086604812", "08086604812", "2348086604812"} {
		cands := phoneCandidates(in)
		if !contains(cands, "+2348086604812") {
			t.Errorf("candidates(%q)=%v missing canonical +2348086604812", in, cands)
		}
		if !contains(cands, "08086604812") {
			t.Errorf("candidates(%q)=%v missing national 08086604812", in, cands)
		}
	}
}

// Without a default country code, E.164 inputs still canonicalise; national
// numbers are left untouched (best-effort, never errors).
func TestCanonicalPhone_NoDefault(t *testing.T) {
	SetDefaultCallingCode("")
	if got := canonicalPhone("+237612345678"); got != "+237612345678" {
		t.Errorf("E.164 should pass through: got %q", got)
	}
	if got := canonicalPhone("00237612345678"); got != "+237612345678" {
		t.Errorf("00-prefix should become +: got %q", got)
	}
	if got := canonicalPhone("0612345678"); got != "0612345678" {
		t.Errorf("national w/o default should stay as-is: got %q", got)
	}
}

func contains(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}
