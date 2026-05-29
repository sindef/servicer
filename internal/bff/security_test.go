package bff

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestVerifiedClientIPIgnoresForwardedForWhenProxyHeadersUntrusted(t *testing.T) {
	t.Setenv("SERVICER_TRUSTED_PROXY_HEADERS", "")
	request := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
	request.RemoteAddr = "10.1.2.3:443"
	request.Header.Set("X-Forwarded-For", "203.0.113.8")
	if got := verifiedClientIP(request); got != "10.1.2.3" {
		t.Fatalf("expected remote address IP when proxy headers untrusted, got %q", got)
	}
}

func TestVerifiedClientIPUsesForwardedForWhenProxyHeadersTrusted(t *testing.T) {
	t.Setenv("SERVICER_TRUSTED_PROXY_HEADERS", "true")
	request := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
	request.RemoteAddr = "10.1.2.3:443"
	request.Header.Set("X-Forwarded-For", "203.0.113.8, 10.0.0.1")
	if got := verifiedClientIP(request); got != "203.0.113.8" {
		t.Fatalf("expected first forwarded IP when proxy headers trusted, got %q", got)
	}
}

func TestLoginRateLimitKeyIncludesProviderUserAndVerifiedIP(t *testing.T) {
	t.Setenv("SERVICER_TRUSTED_PROXY_HEADERS", "true")
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	request.RemoteAddr = "10.1.2.3:443"
	request.Header.Set("X-Forwarded-For", "203.0.113.8")
	key := loginRateLimitKey(request, " Local ", " Alice ")
	if key != "local|alice|203.0.113.8" {
		t.Fatalf("unexpected login rate key %q", key)
	}
}

func TestMemoryLoginLimiterAppliesPerUserLimits(t *testing.T) {
	now := time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC)
	metrics := newServerMetrics()
	limiter := newMemoryLoginLimiter(1, time.Minute, time.Minute, time.Minute, 0, func() time.Time { return now }, metrics)
	keyAlice := "local|alice|203.0.113.8"
	keyBob := "local|bob|203.0.113.8"
	if !limiter.Allow(keyAlice) {
		t.Fatalf("expected first attempt to be allowed")
	}
	limiter.RecordFailure(keyAlice)
	if limiter.Allow(keyAlice) {
		t.Fatalf("expected alice to be throttled after configured failure limit")
	}
	if !limiter.Allow(keyBob) {
		t.Fatalf("expected bob to remain unthrottled on independent key")
	}
	if got := testutil.ToFloat64(metrics.loginRateLimitBlocksTotal); got < 1 {
		t.Fatalf("expected block metric increment, got %f", got)
	}
}

func TestMemoryLoginLimiterCleanupEvictsExpiredEntries(t *testing.T) {
	now := time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC)
	metrics := newServerMetrics()
	limiter := newMemoryLoginLimiter(1, time.Minute, time.Minute, time.Minute, 0, func() time.Time { return now }, metrics)
	key := "local|alice|203.0.113.8"
	limiter.RecordFailure(key)
	if len(limiter.attempts) != 1 {
		t.Fatalf("expected one limiter entry, got %d", len(limiter.attempts))
	}
	now = now.Add(3 * time.Minute)
	_ = limiter.Allow("local|other|203.0.113.8")
	if len(limiter.attempts) != 0 {
		t.Fatalf("expected expired limiter entries to be evicted, got %d", len(limiter.attempts))
	}
	if got := testutil.ToFloat64(metrics.loginRateLimitEvictionsTotal); got < 1 {
		t.Fatalf("expected eviction metric increment, got %f", got)
	}
}

func TestNewLoginLimiterFromEnvFallbackAndProductionGuard(t *testing.T) {
	t.Setenv("SERVICER_PRODUCTION", "")
	t.Setenv(loginRateLimitEnvBackend, "")
	t.Setenv(loginRateLimitEnvAcceptWeakInMemory, "")
	limiter, err := newLoginLimiterFromEnv(newServerMetrics())
	if err != nil || limiter == nil {
		t.Fatalf("expected dev fallback in-memory limiter, got limiter=%v err=%v", limiter, err)
	}

	t.Setenv("SERVICER_PRODUCTION", "true")
	t.Setenv(loginRateLimitEnvBackend, loginRateLimitBackendMemory)
	t.Setenv(loginRateLimitEnvAcceptWeakInMemory, "")
	limiter, err = newLoginLimiterFromEnv(newServerMetrics())
	if err == nil || !strings.Contains(err.Error(), loginRateLimitEnvAcceptWeakInMemory) {
		t.Fatalf("expected production guardrail error, got limiter=%v err=%v", limiter, err)
	}

	t.Setenv(loginRateLimitEnvAcceptWeakInMemory, "true")
	limiter, err = newLoginLimiterFromEnv(newServerMetrics())
	if err != nil || limiter == nil {
		t.Fatalf("expected explicit weak-mode acceptance to allow startup, got limiter=%v err=%v", limiter, err)
	}

	t.Setenv(loginRateLimitEnvBackend, "redis")
	limiter, err = newLoginLimiterFromEnv(newServerMetrics())
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported backend error, got limiter=%v err=%v", limiter, err)
	}
}
