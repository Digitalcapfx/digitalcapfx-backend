package nomba

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// newTestServer wires a fake Nomba API returning the documented envelope shapes.
func newTestServer(t *testing.T, tokenCalls *int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/auth/token/issue", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(tokenCalls, 1)
		if r.Header.Get("accountId") == "" {
			t.Errorf("token issue missing accountId header")
		}
		var body tokenRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.GrantType != "client_credentials" {
			t.Errorf("grant_type = %q, want client_credentials", body.GrantType)
		}
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(tokenEnvelope{
			Code: "00", Description: "Success",
			Data: tokenData{
				BusinessID:   "biz-1",
				AccessToken:  "jwt-access-token",
				RefreshToken: "refresh-abc",
				ExpiresAt:    "2999-01-01T00:00:00.000Z",
			},
		})
	})

	mux.HandleFunc("/v1/accounts/virtual", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer jwt-access-token" {
			t.Errorf("Authorization = %q, want Bearer jwt-access-token", got)
		}
		if r.Header.Get("accountId") == "" {
			t.Errorf("create VA missing accountId header")
		}
		var req CreateVirtualAccountRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": "00", "description": "Success",
			"data": VirtualAccount{
				AccountRef:        req.AccountRef,
				AccountName:       req.AccountName,
				BVN:               req.BVN,
				BankName:          "Nombank MFB",
				BankAccountNumber: "9391076543",
				BankAccountName:   "Nomba/" + req.AccountName,
				Currency:          "NGN",
			},
		})
	})

	mux.HandleFunc("/v1/transfers/banks", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": "00", "description": "Success",
			"data": map[string]any{"results": []Bank{{Code: "058", Name: "Guaranty Trust Bank"}}},
		})
	})

	return httptest.NewServer(mux)
}

func TestCreateVirtualAccount(t *testing.T) {
	var tokenCalls int32
	srv := newTestServer(t, &tokenCalls)
	defer srv.Close()

	c := New("client-id", "client-secret", "acct-uuid", WithBaseURL(srv.URL))
	va, err := c.CreateVirtualAccount(context.Background(), CreateVirtualAccountRequest{
		AccountRef:  "digitalfx-user-1-ngn-0001",
		AccountName: "John Doe Doe",
		BVN:         "12345678901",
	})
	if err != nil {
		t.Fatalf("CreateVirtualAccount: %v", err)
	}
	if va.BankAccountNumber != "9391076543" {
		t.Errorf("bankAccountNumber = %q, want 9391076543", va.BankAccountNumber)
	}
	if va.Currency != "NGN" {
		t.Errorf("currency = %q, want NGN", va.Currency)
	}
	if va.AccountRef != "digitalfx-user-1-ngn-0001" {
		t.Errorf("accountRef round-trip = %q", va.AccountRef)
	}
}

func TestTokenIsCachedAcrossCalls(t *testing.T) {
	var tokenCalls int32
	srv := newTestServer(t, &tokenCalls)
	defer srv.Close()

	c := New("client-id", "client-secret", "acct-uuid", WithBaseURL(srv.URL))
	if _, err := c.CreateVirtualAccount(context.Background(), CreateVirtualAccountRequest{AccountRef: "ref-000000000001", AccountName: "John Doe"}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := c.ListBanks(context.Background()); err != nil {
		t.Fatalf("ListBanks: %v", err)
	}
	if n := atomic.LoadInt32(&tokenCalls); n != 1 {
		t.Errorf("token issued %d times, want 1 (should be cached)", n)
	}
}

func TestListBanks(t *testing.T) {
	var tokenCalls int32
	srv := newTestServer(t, &tokenCalls)
	defer srv.Close()

	c := New("client-id", "client-secret", "acct-uuid", WithBaseURL(srv.URL))
	banks, err := c.ListBanks(context.Background())
	if err != nil {
		t.Fatalf("ListBanks: %v", err)
	}
	if len(banks) != 1 || banks[0].Code != "058" {
		t.Fatalf("banks = %+v, want [{058 ...}]", banks)
	}
}

func TestConfigured(t *testing.T) {
	if New("", "", "").Configured() {
		t.Error("empty client should not be Configured")
	}
	if !New("a", "b", "c").Configured() {
		t.Error("fully-credentialed client should be Configured")
	}
}

func TestVerifyWebhookSignature(t *testing.T) {
	secret := "whsec_test"
	ev := &WebhookEvent{EventType: EventPaymentSuccess, RequestID: "req-1"}
	ev.Data.Merchant.UserID = "user-1"
	ev.Data.Merchant.WalletID = "wallet-1"
	ev.Data.Transaction.TransactionID = "txn-1"
	ev.Data.Transaction.Type = "vact_transfer"
	ev.Data.Transaction.Time = "2026-02-06T10:21:56Z"
	ev.Data.Transaction.ResponseCode = "00"

	// Build a valid signature the same way the verifier does.
	h := http.Header{}
	h.Set(TimestampHeader, "1738837316")
	good := computeSignature(ev, h.Get(TimestampHeader), secret)
	h.Set(SignatureHeader, good)
	if !VerifyWebhookSignature(h, ev, secret) {
		t.Error("valid signature rejected")
	}

	// Tampered event must fail.
	ev.Data.Transaction.Type = "changed"
	if VerifyWebhookSignature(h, ev, secret) {
		t.Error("tampered event accepted")
	}

	// Missing header / empty secret must fail closed.
	if VerifyWebhookSignature(http.Header{}, ev, secret) {
		t.Error("missing signature header accepted")
	}
	if VerifyWebhookSignature(h, ev, "") {
		t.Error("empty secret accepted")
	}
}
