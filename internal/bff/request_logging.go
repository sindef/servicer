package bff

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(recorder, r)
		durationMs := time.Since(start).Milliseconds()
		slog.Info(
			"request completed",
			"method", r.Method,
			"route", requestRoutePattern(r),
			"path", requestPath(r),
			"status", strconv.Itoa(recorder.status),
			"durationMs", durationMs,
			"requestId", strings.TrimSpace(requestHeader(r, requestIDHeader)),
			"correlationId", strings.TrimSpace(requestHeader(r, correlationIDHeader)),
		)
	})
}
