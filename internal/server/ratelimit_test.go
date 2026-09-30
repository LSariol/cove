package server

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/LSariol/Cove/internal/bootstrap"
	"github.com/LSariol/Cove/internal/encryption"
	"github.com/LSariol/Cove/internal/vault"
	"github.com/LSariol/Cove/internal/vault/vaulttest"
)

func TestLimiterBlocksAfterTooManyFailures(t *testing.T) {
	l := newFailureLimiter()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	a, b := netip.MustParseAddr("172.18.0.9"), netip.MustParseAddr("172.18.0.10")

	for i := 0; i < maxFailures-1; i++ {
		l.fail(a, "test")
	}
	if blocked, _ := l.blocked(a); blocked {
		t.Fatal("blocked before reaching the limit")
	}
	l.fail(a, "test")
	if blocked, left := l.blocked(a); !blocked || left != blockDuration {
		t.Fatalf("after %d failures: blocked=%v for %v", maxFailures, blocked, left)
	}
	if blocked, _ := l.blocked(b); blocked {
		t.Fatal("another address was blocked too")
	}

	now = now.Add(blockDuration + time.Second)
	if blocked, _ := l.blocked(a); blocked {
		t.Fatal("still blocked after the block ended")
	}
}

func TestLimiterForgetsOldFailures(t *testing.T) {
	l := newFailureLimiter()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	a := netip.MustParseAddr("172.18.0.9")

	// Slow failures, spread over more than the window, never add up.
	for i := 0; i < 3*maxFailures; i++ {
		l.fail(a, "test")
		now = now.Add(failureWindow / 5)
		if i%5 == 4 {
			now = now.Add(failureWindow)
		}
	}
	if blocked, _ := l.blocked(a); blocked {
		t.Fatal("blocked by failures spread over a long time")
	}
}

func newLimitedAPI(t *testing.T) (*testAPI, *bootstrap.Gate) {
	t.Helper()
	gate := bootstrap.NewGate(t.TempDir(), nil)
	v := vault.New(vaulttest.NewStore(), encryption.NewCipher("k"))
	mux := http.NewServeMux()
	New(v, gate, &fakePinger{}, Options{ClientSecret: testToken}).defineRoutes(mux)
	return &testAPI{t: t, handler: mux, gate: gate}, gate
}

func TestWrongTokensGetRateLimited(t *testing.T) {
	api, _ := newLimitedAPI(t)

	for i := 0; i < maxFailures; i++ {
		code, env := api.do("GET", "/v0/auth", "", "Authorization", "Bearer wrong")
		expectError(t, code, env, 401, "invalid_token")
	}

	// Now the address is refused, even with the right token.
	req := httptest.NewRequest("GET", "/v0/auth", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	api.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after %d wrong tokens: %d, want 429", maxFailures, rec.Code)
	}
	if s, _ := strconv.Atoi(rec.Header().Get("Retry-After")); s < 1 || s > int(blockDuration/time.Second) {
		t.Errorf("Retry-After = %q", rec.Header().Get("Retry-After"))
	}
}

func TestValidTokensAreNeverLimited(t *testing.T) {
	api, _ := newLimitedAPI(t)
	for i := 0; i < 5*maxFailures; i++ {
		if code, _ := api.do("GET", "/v0/auth", "", "Authorization", "Bearer "+testToken); code != 200 {
			t.Fatalf("request %d with a valid token = %d", i+1, code)
		}
	}
}

func TestTokenCheckOutageDoesNotCount(t *testing.T) {
	v := vault.New(vaulttest.NewStore(), encryption.NewCipher("k"))
	mux := http.NewServeMux()
	New(v, bootstrap.NewGate(t.TempDir(), nil), &fakePinger{}, Options{ClientSecret: testToken, Tokens: failingTokens{}}).defineRoutes(mux)
	api := &testAPI{t: t, handler: mux}

	for i := 0; i < 2*maxFailures; i++ {
		code, env := api.do("GET", "/v0/auth", "", "Authorization", "Bearer cove_whatever")
		expectError(t, code, env, 503, "auth_unavailable")
	}
}

func TestRefusedBootstrapsGetRateLimited(t *testing.T) {
	api, gate := newLimitedAPI(t)

	for i := 0; i < maxFailures; i++ {
		code, env := api.do("GET", "/v0/bootstrap/lighthouse", "")
		expectError(t, code, env, 403, "bootstrap_locked")
	}

	// Even once it's opened, the blocked address can't use it (and doesn't
	// use up the window).
	gate.Open(0)
	code, env := api.do("GET", "/v0/bootstrap/lighthouse", "")
	expectError(t, code, env, 429, "too_many_requests")
	if st, _ := gate.Status(); !st.Open {
		t.Fatal("a blocked request used up the bootstrap window")
	}
}
