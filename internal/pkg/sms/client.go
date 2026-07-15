// Package sms provides a thin client for Brevo transactional SMS
// (https://developers.brevo.com/reference/sendtransacsms).
//
// Endpoint: POST https://api.brevo.com/v3/transactionalSMS/sms
// Auth:     api-key header (Brevo API key — NOT the SMTP key)
package sms

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultBaseURL = "https://api.brevo.com/v3"

// Client sends transactional SMS messages via Brevo.
type Client struct {
	apiKey     string
	senderName string // max 11 alphanumeric chars shown on recipient's phone
	baseURL    string
	http       *http.Client
}

// New creates a Client.
//
//   - apiKey:     Brevo v3 API key (Settings → API Keys in Brevo dashboard).
//   - senderName: Alphanumeric sender ID shown to recipients (max 11 chars).
//     Use the app / brand name, e.g. "DigitalFX".
func New(apiKey, senderName string) *Client {
	return &Client{
		apiKey:     apiKey,
		senderName: senderName,
		baseURL:    defaultBaseURL,
		http:       &http.Client{Timeout: 10 * time.Second},
	}
}

// smsRequest is the JSON body accepted by Brevo's transactional SMS endpoint.
type smsRequest struct {
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Content   string `json:"content"`
	Tag       string `json:"tag,omitempty"`
}

// smsResponse is the success body returned by Brevo (we only need the reference).
type smsResponse struct {
	Reference   string `json:"reference"`
	MessageID   int64  `json:"messageId"`
	RemainingCredits float64 `json:"remainingCredits"`
}

// Send delivers a plain-text SMS to recipient (E.164, e.g. "+2348012345678").
// tag is an optional label for analytics / filtering in Brevo (e.g. "otp").
//
// The call is synchronous; wrap in a goroutine for fire-and-forget delivery.
func (c *Client) Send(ctx context.Context, recipient, message, tag string) error {
	payload, err := json.Marshal(smsRequest{
		Sender:    c.senderName,
		Recipient: recipient,
		Content:   message,
		Tag:       tag,
	})
	if err != nil {
		return fmt.Errorf("sms: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/transactionalSMS/sms",
		bytes.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf("sms: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("api-key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("sms: send: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("sms: brevo status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// SendOTP is a convenience wrapper that formats a standard OTP message.
func (c *Client) SendOTP(ctx context.Context, phone, appName, code string) error {
	msg := fmt.Sprintf("Your %s verification code is: %s. Valid for 10 minutes. Do not share this code.", appName, code)
	return c.Send(ctx, phone, msg, "otp")
}
