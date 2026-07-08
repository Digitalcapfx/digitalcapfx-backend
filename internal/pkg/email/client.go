package email

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
)

// Client sends transactional email via Brevo (smtp-relay.brevo.com:587, STARTTLS).
type Client struct {
	host     string
	port     int
	fromName string
	from     string
	user     string // Brevo SMTP login (your Brevo account email)
	password string // Brevo SMTP key (not the master API key)
}

func New(host string, port int, fromName, from, user, password string) *Client {
	return &Client{
		host:     host,
		port:     port,
		fromName: fromName,
		from:     from,
		user:     user,
		password: password,
	}
}

// Send delivers an HTML email. Runs synchronously; call via goroutine for fire-and-forget.
func (c *Client) Send(to, subject, htmlBody string) error {
	addr := net.JoinHostPort(c.host, strconv.Itoa(c.port))

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("email: dial %s: %w", addr, err)
	}

	cl, err := smtp.NewClient(conn, c.host)
	if err != nil {
		return fmt.Errorf("email: smtp client: %w", err)
	}
	defer cl.Close()

	if err := cl.StartTLS(&tls.Config{ServerName: c.host}); err != nil {
		return fmt.Errorf("email: starttls: %w", err)
	}

	if err := cl.Auth(smtp.PlainAuth("", c.user, c.password, c.host)); err != nil {
		return fmt.Errorf("email: auth: %w", err)
	}

	if err := cl.Mail(c.from); err != nil {
		return fmt.Errorf("email: MAIL FROM: %w", err)
	}
	if err := cl.Rcpt(to); err != nil {
		return fmt.Errorf("email: RCPT TO: %w", err)
	}

	w, err := cl.Data()
	if err != nil {
		return fmt.Errorf("email: DATA: %w", err)
	}
	msg := buildRaw(c.fromName, c.from, to, subject, htmlBody)
	if _, err := fmt.Fprint(w, msg); err != nil {
		return fmt.Errorf("email: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email: close data: %w", err)
	}

	return cl.Quit()
}

func buildRaw(fromName, from, to, subject, body string) string {
	var b strings.Builder
	b.WriteString("From: " + fromName + " <" + from + ">\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.String()
}
