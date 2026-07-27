package handlers

// Live integration test for the consolidated crypto-swap path:
//   SwapHandler → SwapService → payments.Client → real payments swap engine.
//
// It is skipped unless PAYMENTS_API_KEY is set, so normal `go test` stays offline.
// Run against the live engine with:
//
//	PAYMENTS_API_KEY=live_sk_... \
//	PAYMENTS_API_URL=https://payments-api-dev-966260606560.europe-west2.run.app \
//	go test -run TestSwapLive -v ./internal/api/handlers/
//
// It exercises the READ paths only (tokens, quote, history). It never executes a
// swap — that spends real crypto and must be done deliberately, not in a test.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/clients/payments"
	"github.com/rachfinance/digitalfx/internal/services"
)

func liveSwapHandler(t *testing.T) *SwapHandler {
	t.Helper()
	key := os.Getenv("PAYMENTS_API_KEY")
	if key == "" {
		t.Skip("PAYMENTS_API_KEY not set — skipping live swap integration test")
	}
	opts := []payments.Option{}
	if u := os.Getenv("PAYMENTS_API_URL"); u != "" {
		opts = append(opts, payments.WithBaseURL(u))
	}
	client := payments.New(key, opts...)
	svc := &services.Services{Swap: services.NewSwapService(client, zap.NewNop())}
	return NewSwapHandler(svc)
}

func TestSwapLive_Tokens(t *testing.T) {
	h := liveSwapHandler(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/swap/tokens?chain=BSC", nil)

	h.GetTokens(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("tokens: got %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, sym := range []string{"USDT", "USDC", "BNB"} {
		if !strings.Contains(body, sym) {
			t.Errorf("tokens: expected %s in BSC registry, body=%s", sym, body)
		}
	}
	t.Logf("tokens OK (%d bytes)", len(body))
}

func TestSwapLive_Quote(t *testing.T) {
	h := liveSwapHandler(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/swap/quote?from_chain=BSC&to_chain=BSC&from_token=USDT&to_token=USDC&amount=1", nil)

	h.GetQuote(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("quote: got %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, field := range []string{"to_amount_expected", "to_amount_min", "platform_fee"} {
		if !strings.Contains(body, field) {
			t.Errorf("quote: missing %s, body=%s", field, body)
		}
	}
	t.Logf("quote OK: %s", body)
}

// TestSwapLive_QuoteCrossChain confirms the bridge (LiFi) path also returns a quote.
func TestSwapLive_QuoteCrossChain(t *testing.T) {
	h := liveSwapHandler(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/swap/quote?from_chain=BSC&to_chain=POL&from_token=USDT&to_token=USDC&amount=5", nil)

	h.GetQuote(rr, req)

	// Cross-chain routes depend on live bridge liquidity; accept a 200 quote or a
	// clean 4xx (e.g. route unavailable) — but never a 5xx/plumbing failure.
	if rr.Code >= 500 {
		t.Fatalf("cross-chain quote returned server error %d: %s", rr.Code, rr.Body.String())
	}
	t.Logf("cross-chain quote status=%d body=%s", rr.Code, rr.Body.String())
}

func TestSwapLive_History(t *testing.T) {
	h := liveSwapHandler(t)
	// Inject an authenticated user the way the real auth middleware does.
	userID := uuid.New()
	if v := os.Getenv("TEST_USER_ID"); v != "" {
		if parsed, err := uuid.Parse(v); err == nil {
			userID = parsed
		}
	}
	ctx := context.WithValue(context.Background(), middleware.ContextKeyUserID, userID)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/swap/history?page=1&limit=20", nil).WithContext(ctx)

	h.GetHistory(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("history: got %d, body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "swaps") {
		t.Errorf("history: expected a swaps field, body=%s", rr.Body.String())
	}
	t.Logf("history OK: %s", rr.Body.String())
}
