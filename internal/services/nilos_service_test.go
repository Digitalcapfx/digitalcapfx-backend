package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"
)

func TestParseNilosDeposit(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantOK  bool
		wantAmt float64
		wantEv  string
	}{
		{
			name:    "top-level inbound success",
			body:    `{"id":"tx-1","accountId":"acc-9","currency":"EUR","amount":100.5,"isInbound":true,"status":"success"}`,
			wantOK:  true,
			wantAmt: 100.5,
			wantEv:  "tx-1",
		},
		{
			name:   "nested data.transaction",
			body:   `{"event":"transaction.created","data":{"transaction":{"id":"tx-2","accountId":"acc-9","currency":"GBP","amount":50,"isInbound":true,"status":"completed"}}}`,
			wantOK: true,
			wantEv: "tx-2",
		},
		{
			name:   "data-only",
			body:   `{"data":{"id":"tx-3","accountId":"acc-9","currency":"EUR","amount":10,"isInbound":true,"status":"settled"}}`,
			wantOK: true,
			wantEv: "tx-3",
		},
		{name: "outbound rejected", body: `{"id":"x","accountId":"a","currency":"EUR","amount":5,"isInbound":false,"status":"success"}`, wantOK: false},
		{name: "non-success rejected", body: `{"id":"x","accountId":"a","currency":"EUR","amount":5,"isInbound":true,"status":"pending"}`, wantOK: false},
		{name: "zero amount rejected", body: `{"id":"x","accountId":"a","currency":"EUR","amount":0,"isInbound":true,"status":"success"}`, wantOK: false},
		{name: "missing accountId rejected", body: `{"id":"x","currency":"EUR","amount":5,"isInbound":true,"status":"success"}`, wantOK: false},
		{name: "missing isInbound rejected", body: `{"id":"x","accountId":"a","currency":"EUR","amount":5,"status":"success"}`, wantOK: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tx, ok := parseNilosDeposit([]byte(c.body))
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if !ok {
				return
			}
			if c.wantAmt != 0 && tx.Amount != c.wantAmt {
				t.Errorf("amount = %v, want %v", tx.Amount, c.wantAmt)
			}
			if c.wantEv != "" && tx.eventID() != c.wantEv {
				t.Errorf("eventID = %q, want %q", tx.eventID(), c.wantEv)
			}
		})
	}
}

func TestVerifyNilosSignature(t *testing.T) {
	secret := []byte("whsec_nilos")
	body := []byte(`{"id":"tx-1","amount":10}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sigHex := hex.EncodeToString(mac.Sum(nil))

	h := http.Header{}
	h.Set("X-Nilos-Signature", sigHex)
	if !verifyNilosSignature(h, body, secret) {
		t.Error("valid hex signature rejected")
	}

	// tampered body
	if verifyNilosSignature(h, []byte(`{"id":"tx-1","amount":9999}`), secret) {
		t.Error("tampered body accepted")
	}
	// missing header
	if verifyNilosSignature(http.Header{}, body, secret) {
		t.Error("missing signature accepted")
	}
}
