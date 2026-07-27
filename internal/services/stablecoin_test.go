package services

import "testing"

// TestStablecoinBranding locks in the customer-facing rename: the CaaS balance is
// now presented as USDC with the name "Stablecoin" (previously iUSD / Instant USD).
func TestStablecoinBranding(t *testing.T) {
	if StablecoinSymbol != "USDC" {
		t.Errorf("StablecoinSymbol = %q, want USDC", StablecoinSymbol)
	}
	if StablecoinName != "Stablecoin" {
		t.Errorf("StablecoinName = %q, want Stablecoin", StablecoinName)
	}
}
