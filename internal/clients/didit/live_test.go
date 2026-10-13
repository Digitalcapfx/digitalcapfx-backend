package didit

import (
	"context"
	"os"
	"strings"
	"testing"
)

// Live read-only probe of the real Didit account. Skipped by default; run with:
//
//	DIDIT_LIVE=1 go test ./internal/clients/didit/ -run TestLive -v
//
// Session creation is not billed, but it is refused when the account has no
// credit — which is itself the thing worth asserting: the client must surface
// that plainly rather than degrade to an unattributable shared link.
func TestLiveCreateSessionSurfacesFailures(t *testing.T) {
	if os.Getenv("DIDIT_LIVE") == "" {
		t.Skip("set DIDIT_LIVE=1 to run against the real Didit API")
	}
	apiKey := os.Getenv("DIDIT_API_KEY")
	if apiKey == "" {
		t.Skip("set DIDIT_API_KEY to run")
	}

	c := New("", apiKey, "", "")
	sessionID, url, err := c.CreateSession(context.Background(), "live-probe-user", "", "")

	if err != nil {
		// The failure must name the cause; a bare "failed" would leave an
		// operator guessing between a bad key, no credit, and a network fault.
		if strings.TrimSpace(err.Error()) == "" {
			t.Fatal("CreateSession failed with an empty error message")
		}
		t.Logf("CreateSession refused (expected when the account has no credit): %v", err)
		if sessionID != "" || url != "" {
			t.Errorf("failed CreateSession still returned sessionID=%q url=%q — it must not hand back a usable link", sessionID, url)
		}
		return
	}

	if url == "" {
		t.Fatal("CreateSession succeeded but returned no url")
	}
	if strings.Contains(url, "ygyeSJcjQaSlwL6YV3xlxA") {
		t.Errorf("CreateSession returned the SHARED hosted link %q — it carries no vendor_data", url)
	}
	t.Logf("session %s -> %s", sessionID, url)
}
