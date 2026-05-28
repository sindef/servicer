package bff

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type loginRateLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	lockout  time.Duration
	attempts map[string]loginAttempt
}

type loginAttempt struct {
	count       int
	windowEnds  time.Time
	lockedUntil time.Time
}

func newLoginRateLimiter(limit int, window, lockout time.Duration) *loginRateLimiter {
	return &loginRateLimiter{limit: limit, window: window, lockout: lockout, attempts: map[string]loginAttempt{}}
}

func (l *loginRateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt := l.attempts[key]
	now := time.Now()
	return attempt.lockedUntil.IsZero() || now.After(attempt.lockedUntil)
}

func (l *loginRateLimiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
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

func (l *loginRateLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func loginRateLimitKey(r *http.Request, provider, username string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + "|" + strings.ToLower(strings.TrimSpace(username)) + "|" + clientIP(r)
}

func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
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
		_ = s.auditStore.persist(ctx, []AuditEventSummary{event})
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SERVICER_AUDIT_STDOUT")), "true") {
		_ = json.NewEncoder(os.Stdout).Encode(event)
	}
}
