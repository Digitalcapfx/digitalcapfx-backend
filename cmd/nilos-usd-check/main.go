// Command nilos-usd-check calls the Nilos account-creation API directly on the
// SWIFT (USD) rail and prints the raw response. It answers one question:
// "does Nilos actually create a USD virtual account for us now?" — independent
// of the DigitalFX app, KYC, or deploy state.
//
// It uses the same signing + credentials as the running service, read from the
// environment (or a local .env): NILOS_BASE_URL, NILOS_API_KEY, NILOS_API_SECRET,
// NILOS_ORG_ID. Run:
//
//	go run ./cmd/nilos-usd-check
//
// A success prints the new Nilos account id + rail details (IBAN / BIC / account
// number). A failure prints the exact Nilos status + body (e.g. "403 Forbidden
// resource"), which tells us the credential/permission still isn't granted.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/rachfinance/digitalfx/internal/clients/nilos"
	"github.com/rachfinance/digitalfx/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Println("failed to load config:", err)
		os.Exit(1)
	}

	fmt.Println("── Nilos USD (SWIFT) account-creation check ──")
	fmt.Println("base URL :", cfg.Nilos.BaseURL)
	fmt.Println("org id   :", orNone(cfg.Nilos.OrgID))
	fmt.Println("api key  :", masked(cfg.Nilos.APIKey))
	fmt.Println("api secret set:", cfg.Nilos.APISecret != "")
	if cfg.Nilos.APIKey == "" || cfg.Nilos.APISecret == "" {
		fmt.Println("\nERROR: NILOS_API_KEY / NILOS_API_SECRET are not set in the environment.")
		fmt.Println("Set them (same values as the Cloud Run service) and re-run.")
		os.Exit(1)
	}

	opts := []nilos.Option{nilos.WithBaseURL(cfg.Nilos.BaseURL), nilos.WithTimeout(30 * time.Second)}
	if cfg.Nilos.OrgID != "" {
		opts = append(opts, nilos.WithOrgID(cfg.Nilos.OrgID))
	}
	client := nilos.New(cfg.Nilos.APIKey, cfg.Nilos.APISecret, opts...)

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()

	// Probe 1 — read permission. If listing accounts works but creating fails,
	// the key is valid but lacks the *account-creation* permission specifically.
	fmt.Println("\n[1/2] GET /v1/accounts (read probe) ...")
	if accs, lerr := client.ListAccounts(ctx); lerr != nil {
		fmt.Println("   ⚠️  read FAILED:", lerr.Error())
	} else {
		fmt.Printf("   ✅ read OK — %d existing account(s) visible to this key\n", len(accs))
		for _, a := range accs {
			curr := ""
			if len(a.Balance) > 0 {
				curr = a.Balance[0].Currency
			}
			fmt.Printf("      • id=%s rail=%s currency=%s name=%q\n", a.ID, a.Rail, curr, a.Name)
		}
	}

	name := fmt.Sprintf("DigitalFX USD SWIFT check %d", time.Now().Unix())
	fmt.Printf("\n[2/2] POST /v1/account  { name: %q, rail: %q } ...\n\n", name, nilos.RailSWIFT)

	acc, err := client.CreateAccount(ctx, nilos.CreateAccountRequest{Name: name, Rail: nilos.RailSWIFT})
	if err != nil {
		fmt.Println("❌ USD account creation FAILED")
		fmt.Println("   ", err.Error())
		var apiErr *nilos.APIError
		if errors.As(err, &apiErr) {
			fmt.Println("    HTTP status:", apiErr.StatusCode)
			fmt.Println("    raw body   :", apiErr.RawBody())
		}
		fmt.Println("\nA 403 means the API key is recognised but still lacks permission to")
		fmt.Println("create accounts (or the wrong org / base URL). This is a Nilos-side")
		fmt.Println("credential/permission fix, not a code fix.")
		os.Exit(1)
	}

	fmt.Println("✅ USD account creation SUCCEEDED — Nilos now provisions USD/SWIFT accounts.")
	fmt.Println("   nilos account id:", acc.ID)
	fmt.Println("   rail            :", acc.Rail)
	if v := acc.DetailString("iban"); v != "" {
		fmt.Println("   iban            :", v)
	}
	if v := acc.DetailString("bic"); v != "" {
		fmt.Println("   bic             :", v)
	}
	if v := acc.DetailString("accountNumber"); v != "" {
		fmt.Println("   accountNumber   :", v)
	}
	raw, _ := json.MarshalIndent(acc.Details, "   ", "  ")
	fmt.Println("   full details    :", string(raw))
}

func masked(s string) string {
	if s == "" {
		return "(empty)"
	}
	if len(s) <= 6 {
		return "***"
	}
	return s[:4] + "…" + s[len(s)-2:]
}

func orNone(s string) string {
	if s == "" {
		return "(not set)"
	}
	return s
}
