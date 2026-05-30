package bff

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

type requestContextKey string

const (
	requestIDHeader      = "X-Request-Id"
	correlationIDHeader  = "X-Correlation-Id"
	requestIDContextKey  = requestContextKey("requestId")
	defaultRequestIDSize = 16
)

func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get(requestIDHeader))
		if requestID == "" {
			requestID = strings.TrimSpace(r.Header.Get("X-Request-ID"))
		}
		if requestID == "" {
			requestID = newRequestID()
		}
		w.Header().Set(requestIDHeader, requestID)
		w.Header().Set(correlationIDHeader, requestID)

		r = r.WithContext(context.WithValue(r.Context(), requestIDContextKey, requestID))
		r.Header.Set(requestIDHeader, requestID)
		next.ServeHTTP(w, r)
	})
}

func requestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(requestIDContextKey).(string)
	return strings.TrimSpace(value)
}

func newRequestID() string {
	buf := make([]byte, defaultRequestIDSize)
	if _, err := rand.Read(buf); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(buf)
}
