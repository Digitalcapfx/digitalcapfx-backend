package didit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

// Expected values were produced by running Didit's documented JavaScript
// canonicalisation (shortenFloats -> sortKeys -> JSON.stringify) over the same
// input. If this drifts, X-Signature-V2 verification silently starts failing
// for every delivery, so the expectations are pinned rather than recomputed.
func TestCanonicalJSONMatchesJSReference(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "keys sorted recursively",
			in:   `{"zeta":1,"alpha":2,"mid":{"b":1,"a":2}}`,
			want: `{"alpha":2,"mid":{"a":2,"b":1},"zeta":1}`,
		},
		{
			name: "whole-number floats shorten",
			in:   `{"score":99.0,"ratio":4.0}`,
			want: `{"ratio":4,"score":99}`,
		},
		{
			// JSON.parse("2.50") is 2.5 and stringifies as "2.5". Preserving the
			// literal "2.50" breaks the signature.
			name: "trailing-zero floats renormalise",
			in:   `{"a":2.50,"b":1.500,"c":0.10}`,
			want: `{"a":2.5,"b":1.5,"c":0.1}`,
		},
		{
			name: "real floats survive",
			in:   `{"a":1.5,"b":99.25}`,
			want: `{"a":1.5,"b":99.25}`,
		},
		{
			// Go escapes < > & by default; JSON.stringify does not.
			name: "html characters are not escaped",
			in:   `{"note":"<b>a&b</b>"}`,
			want: `{"note":"<b>a&b</b>"}`,
		},
		{
			name: "unicode stays unescaped",
			in:   `{"name":"José ✓"}`,
			want: `{"name":"José ✓"}`,
		},
		{
			name: "array order is preserved",
			in:   `{"l":[3,1,2]}`,
			want: `{"l":[3,1,2]}`,
		},
		{
			// Integer literals are kept verbatim rather than round-tripped through
			// float64. This follows Didit's Python server (arbitrary-precision
			// ints) rather than the JS snippet, which would round 2^53+1 down.
			// Academic for KYC payloads; the point is that no precision is lost.
			name: "large integers keep precision",
			in:   `{"id":9007199254740993,"ts":1774970000}`,
			want: `{"id":9007199254740993,"ts":1774970000}`,
		},
		{
			name: "null and bool pass through",
			in:   `{"b":true,"a":null}`,
			want: `{"a":null,"b":true}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := canonicalJSON([]byte(c.in))
			if err != nil {
				t.Fatalf("canonicalJSON: %v", err)
			}
			if string(got) != c.want {
				t.Errorf("canonicalJSON(%s)\n got: %s\nwant: %s", c.in, got, c.want)
			}
		})
	}
}

func TestVerifyWebhookSignatureFailsClosed(t *testing.T) {
	c := New("", "", "", "") // no webhook secret
	body := []byte(`{"status":"Approved","vendor_data":"victim"}`)

	if c.VerifyWebhookSignature("", "", body) {
		t.Error("accepted an unsigned webhook with no secret configured")
	}
	if c.VerifyWebhookSignature("deadbeef", "deadbeef", body) {
		t.Error("accepted a signed webhook with no secret configured")
	}
}

func TestVerifyWebhookSignatureAcceptsBothSchemes(t *testing.T) {
	const secret = "s3cret"
	c := New("", "", secret, "")

	// Deliberately not in canonical key order, and with a trailing-zero float.
	raw := []byte(`{"status":"Approved","event_id":"e1","score":2.50}`)

	canonical, err := canonicalJSON(raw)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}

	t.Run("X-Signature-V2 over canonical body", func(t *testing.T) {
		if !c.VerifyWebhookSignature(sign(secret, canonical), "", raw) {
			t.Error("genuine X-Signature-V2 rejected")
		}
	})

	t.Run("X-Signature over raw bytes", func(t *testing.T) {
		if !c.VerifyWebhookSignature("", sign(secret, raw), raw) {
			t.Error("genuine X-Signature rejected")
		}
	})

	t.Run("wrong secret rejected", func(t *testing.T) {
		if c.VerifyWebhookSignature(sign("other", canonical), sign("other", raw), raw) {
			t.Error("signature from the wrong secret accepted")
		}
	})

	t.Run("malformed hex rejected", func(t *testing.T) {
		if c.VerifyWebhookSignature("not-hex", "not-hex", raw) {
			t.Error("malformed signature accepted")
		}
	})
}

func TestIsFresh(t *testing.T) {
	now := time.Unix(1_774_970_000, 0)

	cases := []struct {
		name string
		ts   int64
		want bool
	}{
		{"now", now.Unix(), true},
		{"4 minutes old", now.Add(-4 * time.Minute).Unix(), true},
		{"6 minutes old", now.Add(-6 * time.Minute).Unix(), false},
		{"6 minutes in the future", now.Add(6 * time.Minute).Unix(), false},
		{"missing", 0, false},
		{"negative", -1, false},
	}

	for _, c := range cases {
		if got := IsFresh(c.ts, now); got != c.want {
			t.Errorf("%s: IsFresh(%d) = %v, want %v", c.name, c.ts, got, c.want)
		}
	}
}

func TestConfigured(t *testing.T) {
	if New("", "", "", "").Configured() {
		t.Error("client with no API key reported as configured")
	}
	if New("", "   ", "", "").Configured() {
		t.Error("client with whitespace API key reported as configured")
	}
	if !New("", "key", "", "").Configured() {
		t.Error("client with an API key reported as unconfigured")
	}
}

func sign(secret string, msg []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(msg)
	return hex.EncodeToString(h.Sum(nil))
}
