// Raw Brevo SMTP auth diagnostic: connects, STARTTLS, and tries AUTH LOGIN
// with the provided credentials, printing every server response so we can see
// exactly why 535 happens (bad key vs IP restriction vs mechanism).
package main

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net/smtp"
	"os"
)

// loginAuth implements the AUTH LOGIN mechanism (Brevo supports PLAIN and LOGIN).
type loginAuth struct{ user, pass string }

func (a *loginAuth) Start(_ *smtp.ServerInfo) (string, []byte, error) {
	return "LOGIN", nil, nil
}
func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch string(fromServer) {
	case "Username:":
		return []byte(a.user), nil
	case "Password:":
		return []byte(a.pass), nil
	default:
		return nil, fmt.Errorf("unexpected server challenge: %q", fromServer)
	}
}

func main() {
	host := "smtp-relay.brevo.com"
	user := os.Getenv("BREVO_SMTP_USER")
	pass := os.Getenv("BREVO_SMTP_KEY")
	fmt.Printf("host=%s port=587\nuser=%q (len %d)\npass len=%d, b64=%s\n\n",
		host, user, len(user), len(pass), base64.StdEncoding.EncodeToString([]byte(pass)))

	c, err := smtp.Dial(host + ":587")
	if err != nil {
		fmt.Println("dial:", err)
		os.Exit(1)
	}
	defer c.Close()
	if err := c.Hello("digitalfx-diag"); err != nil {
		fmt.Println("ehlo:", err)
	}
	if ok, ext := c.Extension("STARTTLS"); ok {
		fmt.Println("STARTTLS advertised:", ext)
	}
	if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
		fmt.Println("starttls:", err)
		os.Exit(1)
	}
	if ok, mechs := c.Extension("AUTH"); ok {
		fmt.Println("AUTH mechanisms:", mechs)
	}

	fmt.Println("\n--- trying PLAIN ---")
	if err := c.Auth(smtp.PlainAuth("", user, pass, host)); err != nil {
		fmt.Println("PLAIN failed:", err)
	} else {
		fmt.Println("PLAIN OK")
		return
	}

	// Reconnect for a clean LOGIN attempt (some servers close after auth fail).
	c.Close()
	c2, err := smtp.Dial(host + ":587")
	if err != nil {
		fmt.Println("redial:", err)
		os.Exit(1)
	}
	defer c2.Close()
	_ = c2.Hello("digitalfx-diag")
	_ = c2.StartTLS(&tls.Config{ServerName: host})
	fmt.Println("\n--- trying LOGIN ---")
	if err := c2.Auth(&loginAuth{user, pass}); err != nil {
		fmt.Println("LOGIN failed:", err)
		os.Exit(1)
	}
	fmt.Println("LOGIN OK")
}
