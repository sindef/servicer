package bff

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	loginRateLimitDefaultLimit          = 5
	loginRateLimitDefaultWindow         = 15 * time.Minute
	loginRateLimitDefaultLockout        = 15 * time.Minute
	loginRateLimitDefaultCleanupEvery   = 1 * time.Minute
	loginRateLimitDefaultRetention      = 30 * time.Minute
	loginRateLimitBackendMemory         = "memory"
	loginRateLimitEnvBackend            = "SERVICER_LOGIN_RATE_LIMIT_BACKEND"
	loginRateLimitEnvAcceptWeakInMemory = "SERVICER_LOGIN_RATE_LIMIT_ACCEPT_IN_MEMORY"
)

type loginLimiter interface {
	Allow(key string) bool
	RecordFailure(key string)
	Reset(key string)
}

type memoryLoginLimiter struct {
	mu        sync.Mutex
	limit     int
	window    time.Duration
	lockout   time.Duration
	retain    time.Duration
	cleanup   time.Duration
	lastSweep time.Time
	nowFn     func() time.Time
	metrics   *serverMetrics
	attempts  map[string]loginAttempt
}

type loginAttempt struct {
	count       int
	windowEnds  time.Time
	lockedUntil time.Time
}

func newLoginLimiterFromEnv(metrics *serverMetrics) (loginLimiter, error) {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv(loginRateLimitEnvBackend)))
	if backend == "" {
		backend = loginRateLimitBackendMemory
	}
	switch backend {
	case loginRateLimitBackendMemory:
		accepted := strings.EqualFold(strings.TrimSpace(os.Getenv(loginRateLimitEnvAcceptWeakInMemory)), "true")
		if productionMode() && !accepted {
			return nil, fmt.Errorf("%s=%s is unsafe with multiple BFF replicas; set %s=true only if you explicitly accept weak per-replica throttling", loginRateLimitEnvBackend, loginRateLimitBackendMemory, loginRateLimitEnvAcceptWeakInMemory)
		}
		if productionMode() && accepted {
			slog.Warn("using in-memory login throttling in production; this is per-replica and should be replaced with a shared limiter backend")
		}
		return newMemoryLoginLimiter(
			loginRateLimitDefaultLimit,
			loginRateLimitDefaultWindow,
			loginRateLimitDefaultLockout,
			loginRateLimitDefaultRetention,
			loginRateLimitDefaultCleanupEvery,
			time.Now,
			metrics,
		), nil
	default:
		return nil, fmt.Errorf("unsupported login limiter backend %q", backend)
	}
}

func newMemoryLoginLimiter(limit int, window, lockout, retain, cleanup time.Duration, nowFn func() time.Time, metrics *serverMetrics) *memoryLoginLimiter {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &memoryLoginLimiter{
		limit:    limit,
		window:   window,
		lockout:  lockout,
		retain:   retain,
		cleanup:  cleanup,
		nowFn:    nowFn,
		metrics:  metrics,
		attempts: map[string]loginAttempt{},
	}
}

func (l *memoryLoginLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.cleanupExpired(now)
	attempt := l.attempts[key]
	allowed := attempt.lockedUntil.IsZero() || now.After(attempt.lockedUntil)
	if !allowed && l.metrics != nil {
		l.metrics.loginRateLimitBlocksTotal.Inc()
	}
	return allowed
}

func (l *memoryLoginLimiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.cleanupExpired(now)
	attempt := l.attempts[key]
	if attempt.windowEnds.IsZero() || now.After(attempt.windowEnds) {
		attempt = loginAttempt{windowEnds: now.Add(l.window)}
	}
	attempt.count++
	if attempt.count >= l.limit {
		attempt.lockedUntil = now.Add(l.lockout)
	}
	l.attempts[key] = attempt
}

func (l *memoryLoginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
	l.cleanupExpired(l.now())
}

func loginRateLimitKey(r *http.Request, provider, username string) string {
	providerName := strings.ToLower(strings.TrimSpace(provider))
	if providerName == "" {
		providerName = "unknown-provider"
	}
	userName := strings.ToLower(strings.TrimSpace(username))
	if userName == "" {
		userName = "unknown-user"
	}
	return providerName + "|" + userName + "|" + verifiedClientIP(r)
}

func verifiedClientIP(r *http.Request) string {
	if r == nil {
		return "unknown-ip"
	}
	if trustedProxyHeadersEnabled() {
		if forwarded := firstForwardedHeaderValue(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			if ip := net.ParseIP(strings.TrimSpace(forwarded)); ip != nil {
				return ip.String()
			}
		}
	}
	if host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		if ip := net.ParseIP(strings.TrimSpace(host)); ip != nil {
			return ip.String()
		}
	}
	if ip := net.ParseIP(strings.TrimSpace(r.RemoteAddr)); ip != nil {
		return ip.String()
	}
	if trimmed := strings.TrimSpace(r.RemoteAddr); trimmed != "" {
		return trimmed
	}
	return "unknown-ip"
}

func (l *memoryLoginLimiter) now() time.Time {
	return l.nowFn().UTC()
}

func (l *memoryLoginLimiter) cleanupExpired(now time.Time) {
	if l.cleanup > 0 && !l.lastSweep.IsZero() && now.Sub(l.lastSweep) < l.cleanup {
		return
	}
	if l.cleanup > 0 {
		l.lastSweep = now
	}
	removed := 0
	for key, attempt := range l.attempts {
		expiry := attempt.windowEnds
		if attempt.lockedUntil.After(expiry) {
			expiry = attempt.lockedUntil
		}
		if l.retain > 0 {
			expiry = expiry.Add(l.retain)
		}
		if !expiry.IsZero() && now.After(expiry) {
			delete(l.attempts, key)
			removed++
		}
	}
	if removed > 0 && l.metrics != nil {
		l.metrics.loginRateLimitEvictionsTotal.Add(float64(removed))
	}
}

func requiresCSRF(r *http.Request) bool {
	if r == nil {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	if strings.HasPrefix(r.URL.Path, "/api/auth/") {
		return false
	}
	if authorizationBearerToken(r.Header.Get("Authorization")) != "" {
		return false
	}
	_, err := r.Cookie(authSessionCookieName)
	return err == nil
}

func secureCompare(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func (s *Server) ensureCSRFCookie(w http.ResponseWriter, r *http.Request) {
	ensureCSRFCookieForRequest(w, r)
}

func ensureCSRFCookieForRequest(w http.ResponseWriter, r *http.Request) {
	if _, err := r.Cookie(csrfCookieName); err == nil {
		return
	}
	token, err := randomString(32)
	if err != nil {
		return
	}
	http.SetCookie(w, csrfCookie(r, token))
}

func (s *Server) recordAudit(ctx context.Context, event AuditEventSummary) {
	if s.auditStore != nil {
		if err := s.auditStore.persist(ctx, []AuditEventSummary{event}); err != nil {
			if s.metrics != nil {
				s.metrics.auditPersistFailuresTotal.Inc()
			}
			slog.Error("failed to persist audit event", "error", err.Error(), "type", event.Type, "subject", event.Subject, "requestId", event.RequestID)
		}
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SERVICER_AUDIT_STDOUT")), "true") {
		_ = json.NewEncoder(os.Stdout).Encode(event)
	}
}
