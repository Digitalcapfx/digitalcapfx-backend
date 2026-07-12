// smtp-test verifies Brevo SMTP credentials and sender acceptance using the
// exact production email client, so we catch sender-verification problems
// before touching the live service.
//
// Usage: go run ./cmd/smtp-test <from-email> <to-email>
package main

import (
	"fmt"
	"os"

	"github.com/rachfinance/digitalfx/internal/pkg/email"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("usage: go run ./cmd/smtp-test <from-email> <to-email>")
		os.Exit(2)
	}
	from, to := os.Args[1], os.Args[2]

	host := envOr("BREVO_SMTP_HOST", "smtp-relay.brevo.com")
	user := envOr("BREVO_SMTP_USER", "")
	pass := envOr("BREVO_SMTP_KEY", "")

	c := email.New(host, 587, "DigitalFX", from, user, pass)

	subject := "DigitalFX SMTP test"
	body := "<p>If you can read this, Brevo SMTP is working. Your OTP delivery is unblocked.</p>"

	fmt.Printf("sending FROM %q TO %q via %s (user=%s)...\n", from, to, host, user)
	if err := c.Send(to, subject, body); err != nil {
		fmt.Printf("FAILED: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("SUCCESS — Brevo accepted the message.")
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
