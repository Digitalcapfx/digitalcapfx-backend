// Package handlers_test validates HTTP routing and middleware enforcement
// without requiring a live DB or Redis connection.
//
// Strategy: build a test chi router that uses the real middleware.Auth and
// middleware.AdminOnly with a known secret, but registers stub 200-OK handlers
// in place of the real ones. This lets us verify:
//   - every protected route returns 401 when called without a JWT
//   - every protected route returns 401 when called with an invalid JWT
//   - admin routes return 403 when called with a valid but non-admin JWT
//   - public routes return 200 (or at least not 401) without auth
package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
)

// ── JWT helpers ───────────────────────────────────────────────────────────────

const routeTestSecret = "routes-test-jwt-secret"

func signToken(t *testing.T, userID uuid.UUID, role string) string {
	t.Helper()
	claims := middleware.Claims{
		UserID: userID.String(),
		Phone:  "+237612345678",
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "access",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(routeTestSecret))
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}
	return tok
}

// ── Test router ───────────────────────────────────────────────────────────────

func buildTestRouter() http.Handler {
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

	r := chi.NewRouter()

	// ── Public ───────────────────────────────────────────────────────────────
	r.Get("/health", ok)
	r.Post("/webhooks/hub2", ok)
	r.Post("/webhooks/metamap", ok)

	r.Post("/api/v1/auth/otp/send", ok)
	r.Post("/api/v1/auth/otp/verify", ok)
	r.Post("/api/v1/auth/register", ok)
	r.Post("/api/v1/auth/login", ok)
	r.Post("/api/v1/auth/2fa/login", ok)
	r.Post("/api/v1/auth/google", ok)
	r.Post("/api/v1/auth/token/refresh", ok)
	r.Post("/api/v1/auth/forgot-pin", ok)
	r.Post("/api/v1/auth/reset-pin", ok)
	r.Get("/api/v1/support/links", ok)

	// ── Protected (JWT required) ──────────────────────────────────────────────
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(routeTestSecret))

		// Auth session management
		r.Post("/api/v1/auth/logout", ok)
		r.Post("/api/v1/auth/email/resend-otp", ok)
		r.Post("/api/v1/auth/email/verify", ok)
		r.Get("/api/v1/auth/devices", ok)
		r.Delete("/api/v1/auth/devices", ok)
		r.Delete("/api/v1/auth/devices/{id}", ok)

		// Profile + preferences
		r.Get("/api/v1/profile", ok)
		r.Patch("/api/v1/profile", ok)
		r.Get("/api/v1/profile/preferences", ok)
		r.Patch("/api/v1/profile/preferences", ok)

		// Security
		r.Get("/api/v1/security", ok)
		r.Post("/api/v1/security/2fa/setup", ok)
		r.Post("/api/v1/security/2fa/confirm", ok)
		r.Delete("/api/v1/security/2fa", ok)
		r.Post("/api/v1/security/pin/change", ok)
		r.Post("/api/v1/security/biometrics/enable", ok)
		r.Delete("/api/v1/security/biometrics", ok)

		// Support (authenticated)
		r.Get("/api/v1/support/faqs", ok)
		r.Post("/api/v1/support/tickets", ok)
		r.Get("/api/v1/support/tickets", ok)
		r.Get("/api/v1/support/tickets/{id}", ok)
		r.Post("/api/v1/support/tickets/{id}/messages", ok)

		// KYC
		r.Get("/api/v1/kyc/status", ok)
		r.Get("/api/v1/kyc/documents", ok)
		r.Post("/api/v1/kyc/documents", ok)
		r.Post("/api/v1/kyc/metamap/init", ok)

		// Notifications
		r.Get("/api/v1/notifications", ok)
		r.Get("/api/v1/notifications/unread-count", ok)
		r.Patch("/api/v1/notifications/read-all", ok)
		r.Patch("/api/v1/notifications/{id}/read", ok)

		// Financial routes (KYC middleware skipped in test — stub only)
		r.Get("/api/v1/accounts/", ok)
		r.Get("/api/v1/accounts/{currency}", ok)
		r.Get("/api/v1/accounts/{currency}/transactions", ok)
		r.Get("/api/v1/accounts/{currency}/transactions/{id}", ok)

		r.Get("/api/v1/wallets/", ok)
		r.Post("/api/v1/wallets/", ok)
		r.Get("/api/v1/wallets/{walletId}/address", ok)
		r.Post("/api/v1/wallets/deposit", ok)
		r.Post("/api/v1/wallets/withdraw", ok)

		r.Get("/api/v1/crypto/wallet", ok)
		r.Post("/api/v1/crypto/fund", ok)
		r.Get("/api/v1/crypto/balances", ok)
		r.Post("/api/v1/crypto/send", ok)
		r.Get("/api/v1/crypto/transactions", ok)
		r.Get("/api/v1/crypto/transactions/{id}", ok)

		r.Post("/api/v1/transfers/internal", ok)
		r.Post("/api/v1/transfers/hub2", ok)
		r.Post("/api/v1/transfers/exchange", ok)

		r.Get("/api/v1/dashboard", ok)
		r.Get("/api/v1/activity", ok)
		r.Get("/api/v1/insights", ok)
		r.Get("/api/v1/crypto/contacts", ok)

		r.Get("/api/v1/exchange/rate", ok)
		r.Post("/api/v1/exchange/quote", ok)
		r.Post("/api/v1/exchange/execute", ok)
		r.Get("/api/v1/exchange/history", ok)

		r.Get("/api/v1/wallets/overview", ok)
		r.Get("/api/v1/wallets/supported-assets", ok)
		r.Get("/api/v1/wallets/fiat/{currency}", ok)
		r.Get("/api/v1/wallets/fiat/{currency}/transactions", ok)
		r.Get("/api/v1/wallets/crypto/{network}", ok)
		r.Get("/api/v1/wallets/crypto/{network}/transactions", ok)
		r.Get("/api/v1/wallets/stablecoin/{symbol}", ok)
		r.Get("/api/v1/wallets/stablecoin/{symbol}/transactions", ok)

		r.Post("/api/v1/withdrawals/quote", ok)
		r.Post("/api/v1/withdrawals", ok)
		r.Get("/api/v1/withdrawals", ok)
		r.Get("/api/v1/withdrawals/{id}", ok)
		r.Get("/api/v1/withdrawals/beneficiaries", ok)
		r.Post("/api/v1/withdrawals/beneficiaries", ok)
		r.Delete("/api/v1/withdrawals/beneficiaries/{id}", ok)

		// Admin routes
		r.Group(func(r chi.Router) {
			r.Use(middleware.AdminOnly)
			r.Get("/api/v1/admin/kyc/pending", ok)
			r.Post("/api/v1/admin/kyc/{id}/approve", ok)
			r.Post("/api/v1/admin/kyc/{id}/reject", ok)
			r.Post("/api/v1/admin/withdrawal-rates", ok)
			r.Get("/api/v1/admin/withdrawal-rates", ok)
		})
	})

	return r
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func do(t *testing.T, h http.Handler, method, path, bearer string) int {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Code
}

// ── Route tables ──────────────────────────────────────────────────────────────

// publicRoutes must be accessible without any JWT.
var publicRoutes = []struct{ method, path string }{
	{"GET", "/health"},
	{"POST", "/api/v1/auth/otp/send"},
	{"POST", "/api/v1/auth/otp/verify"},
	{"POST", "/api/v1/auth/register"},
	{"POST", "/api/v1/auth/login"},
	{"POST", "/api/v1/auth/2fa/login"},
	{"POST", "/api/v1/auth/google"},
	{"POST", "/api/v1/auth/token/refresh"},
	{"POST", "/api/v1/auth/forgot-pin"},
	{"POST", "/api/v1/auth/reset-pin"},
	{"GET", "/api/v1/support/links"},
	{"POST", "/webhooks/hub2"},
	{"POST", "/webhooks/metamap"},
}

// protectedRoutes require a valid JWT (any role).
var protectedRoutes = []struct{ method, path string }{
	// auth session
	{"POST", "/api/v1/auth/logout"},
	{"POST", "/api/v1/auth/email/resend-otp"},
	{"POST", "/api/v1/auth/email/verify"},
	{"GET", "/api/v1/auth/devices"},
	{"DELETE", "/api/v1/auth/devices"},
	{"DELETE", "/api/v1/auth/devices/abc123"},
	// profile
	{"GET", "/api/v1/profile"},
	{"PATCH", "/api/v1/profile"},
	{"GET", "/api/v1/profile/preferences"},
	{"PATCH", "/api/v1/profile/preferences"},
	// security
	{"GET", "/api/v1/security"},
	{"POST", "/api/v1/security/2fa/setup"},
	{"POST", "/api/v1/security/2fa/confirm"},
	{"DELETE", "/api/v1/security/2fa"},
	{"POST", "/api/v1/security/pin/change"},
	{"POST", "/api/v1/security/biometrics/enable"},
	{"DELETE", "/api/v1/security/biometrics"},
	// support
	{"GET", "/api/v1/support/faqs"},
	{"POST", "/api/v1/support/tickets"},
	{"GET", "/api/v1/support/tickets"},
	{"GET", "/api/v1/support/tickets/ticket-id"},
	{"POST", "/api/v1/support/tickets/ticket-id/messages"},
	// kyc
	{"GET", "/api/v1/kyc/status"},
	{"GET", "/api/v1/kyc/documents"},
	{"POST", "/api/v1/kyc/documents"},
	{"POST", "/api/v1/kyc/metamap/init"},
	// notifications
	{"GET", "/api/v1/notifications"},
	{"GET", "/api/v1/notifications/unread-count"},
	{"PATCH", "/api/v1/notifications/read-all"},
	{"PATCH", "/api/v1/notifications/notif-id/read"},
	// accounts
	{"GET", "/api/v1/accounts/"},
	{"GET", "/api/v1/accounts/USD"},
	{"GET", "/api/v1/accounts/USD/transactions"},
	{"GET", "/api/v1/accounts/USD/transactions/tx-id"},
	// wallets (custody)
	{"GET", "/api/v1/wallets/"},
	{"POST", "/api/v1/wallets/"},
	{"GET", "/api/v1/wallets/wallet-id/address"},
	{"POST", "/api/v1/wallets/deposit"},
	{"POST", "/api/v1/wallets/withdraw"},
	// crypto (CaaS)
	{"GET", "/api/v1/crypto/wallet"},
	{"POST", "/api/v1/crypto/fund"},
	{"GET", "/api/v1/crypto/balances"},
	{"POST", "/api/v1/crypto/send"},
	{"GET", "/api/v1/crypto/transactions"},
	{"GET", "/api/v1/crypto/transactions/tx-id"},
	// transfers
	{"POST", "/api/v1/transfers/internal"},
	{"POST", "/api/v1/transfers/hub2"},
	{"POST", "/api/v1/transfers/exchange"},
	// dashboard + feed + insights
	{"GET", "/api/v1/dashboard"},
	{"GET", "/api/v1/activity"},
	{"GET", "/api/v1/insights"},
	{"GET", "/api/v1/crypto/contacts"},
	// exchange
	{"GET", "/api/v1/exchange/rate"},
	{"POST", "/api/v1/exchange/quote"},
	{"POST", "/api/v1/exchange/execute"},
	{"GET", "/api/v1/exchange/history"},
	// wallet overview
	{"GET", "/api/v1/wallets/overview"},
	{"GET", "/api/v1/wallets/supported-assets"},
	{"GET", "/api/v1/wallets/fiat/USD"},
	{"GET", "/api/v1/wallets/fiat/USD/transactions"},
	{"GET", "/api/v1/wallets/crypto/ETHEREUM"},
	{"GET", "/api/v1/wallets/crypto/ETHEREUM/transactions"},
	{"GET", "/api/v1/wallets/stablecoin/USDC"},
	{"GET", "/api/v1/wallets/stablecoin/USDC/transactions"},
	// withdrawals
	{"POST", "/api/v1/withdrawals/quote"},
	{"POST", "/api/v1/withdrawals"},
	{"GET", "/api/v1/withdrawals"},
	{"GET", "/api/v1/withdrawals/w-id"},
	{"GET", "/api/v1/withdrawals/beneficiaries"},
	{"POST", "/api/v1/withdrawals/beneficiaries"},
	{"DELETE", "/api/v1/withdrawals/beneficiaries/b-id"},
}

// adminRoutes require admin role specifically.
var adminRoutes = []struct{ method, path string }{
	{"GET", "/api/v1/admin/kyc/pending"},
	{"POST", "/api/v1/admin/kyc/user-id/approve"},
	{"POST", "/api/v1/admin/kyc/user-id/reject"},
	{"POST", "/api/v1/admin/withdrawal-rates"},
	{"GET", "/api/v1/admin/withdrawal-rates"},
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestPublicRoutes_NoAuth verifies that none of the public routes require a JWT.
func TestPublicRoutes_NoAuth(t *testing.T) {
	h := buildTestRouter()
	for _, rt := range publicRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			status := do(t, h, rt.method, rt.path, "")
			if status == http.StatusUnauthorized {
				t.Errorf("public route returned 401 — should be accessible without auth")
			}
		})
	}
}

// TestProtectedRoutes_NoToken verifies that all protected routes reject requests
// that carry no Authorization header with HTTP 401.
func TestProtectedRoutes_NoToken(t *testing.T) {
	h := buildTestRouter()
	for _, rt := range protectedRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			if got := do(t, h, rt.method, rt.path, ""); got != http.StatusUnauthorized {
				t.Errorf("expected 401 without token, got %d", got)
			}
		})
	}
}

// TestProtectedRoutes_InvalidToken verifies that tampered / wrong-secret tokens
// are rejected with HTTP 401.
func TestProtectedRoutes_InvalidToken(t *testing.T) {
	h := buildTestRouter()
	for _, rt := range protectedRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			if got := do(t, h, rt.method, rt.path, "totally.invalid.token"); got != http.StatusUnauthorized {
				t.Errorf("expected 401 with invalid token, got %d", got)
			}
		})
	}
}

// TestProtectedRoutes_ValidToken verifies that a valid JWT lets requests through
// the auth middleware (handler returns 200 from the stub).
func TestProtectedRoutes_ValidToken(t *testing.T) {
	h := buildTestRouter()
	userToken := signToken(t, uuid.New(), "user")
	for _, rt := range protectedRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			if got := do(t, h, rt.method, rt.path, userToken); got != http.StatusOK {
				t.Errorf("expected 200 with valid token, got %d", got)
			}
		})
	}
}

// TestAdminRoutes_NoToken verifies admin routes return 401 without any token.
func TestAdminRoutes_NoToken(t *testing.T) {
	h := buildTestRouter()
	for _, rt := range adminRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			if got := do(t, h, rt.method, rt.path, ""); got != http.StatusUnauthorized {
				t.Errorf("expected 401 without token, got %d", got)
			}
		})
	}
}

// TestAdminRoutes_UserRole verifies admin routes return 403 for valid non-admin JWTs.
func TestAdminRoutes_UserRole(t *testing.T) {
	h := buildTestRouter()
	userToken := signToken(t, uuid.New(), "user")
	for _, rt := range adminRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			if got := do(t, h, rt.method, rt.path, userToken); got != http.StatusForbidden {
				t.Errorf("expected 403 for non-admin role, got %d", got)
			}
		})
	}
}

// TestAdminRoutes_AdminRole verifies admin routes return 200 for admin JWTs.
func TestAdminRoutes_AdminRole(t *testing.T) {
	h := buildTestRouter()
	adminToken := signToken(t, uuid.New(), "admin")
	for _, rt := range adminRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			if got := do(t, h, rt.method, rt.path, adminToken); got != http.StatusOK {
				t.Errorf("expected 200 for admin role, got %d", got)
			}
		})
	}
}

// TestAdminRoutes_ExpiredToken verifies expired JWTs are rejected.
func TestAdminRoutes_ExpiredToken(t *testing.T) {
	h := buildTestRouter()
	claims := middleware.Claims{
		UserID: uuid.New().String(),
		Phone:  "+237612345678",
		Role:   "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "access",
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)), // already expired
		},
	}
	expired, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(routeTestSecret))

	for _, rt := range adminRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			if got := do(t, h, rt.method, rt.path, expired); got != http.StatusUnauthorized {
				t.Errorf("expected 401 for expired token, got %d", got)
			}
		})
	}
}

// TestWrongSecret rejects a JWT signed with a different secret.
func TestWrongSecret(t *testing.T) {
	h := buildTestRouter()
	// Sign with a DIFFERENT secret than routeTestSecret.
	claims := middleware.Claims{
		UserID: uuid.New().String(),
		Phone:  "+237612345678",
		Role:   "user",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "access",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	wrongSecretToken, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("wrong-secret"))
	if got := do(t, h, "GET", "/api/v1/profile", wrongSecretToken); got != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong-secret token, got %d", got)
	}
}

// TestBearerPrefixRequired ensures that the Authorization header must use "Bearer " prefix.
func TestBearerPrefixRequired(t *testing.T) {
	h := buildTestRouter()
	tok := signToken(t, uuid.New(), "user")
	// Send without "Bearer " prefix
	req := httptest.NewRequest("GET", "/api/v1/profile", nil)
	req.Header.Set("Authorization", tok) // no "Bearer " prefix
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without Bearer prefix, got %d", rr.Code)
	}
}
