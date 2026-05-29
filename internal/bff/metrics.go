package bff

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type serverMetrics struct {
	registry                     *prometheus.Registry
	requestsTotal                *prometheus.CounterVec
	requestDuration              *prometheus.HistogramVec
	authFailuresTotal            prometheus.Counter
	upstreamFailuresTotal        prometheus.Counter
	loginRateLimitBlocksTotal    prometheus.Counter
	loginRateLimitEvictionsTotal prometheus.Counter
}

func newServerMetrics() *serverMetrics {
	registry := prometheus.NewRegistry()
	metrics := &serverMetrics{
		registry: registry,
		requestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "servicer",
				Subsystem: "bff",
				Name:      "http_requests_total",
				Help:      "Total number of HTTP requests handled by the Servicer BFF.",
			},
			[]string{"method", "route", "status"},
		),
		requestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "servicer",
				Subsystem: "bff",
				Name:      "http_request_duration_seconds",
				Help:      "Latency of HTTP requests handled by the Servicer BFF.",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"method", "route", "status"},
		),
		authFailuresTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: "servicer",
				Subsystem: "bff",
				Name:      "authentication_failures_total",
				Help:      "Total number of failed authentication attempts.",
			},
		),
		upstreamFailuresTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: "servicer",
				Subsystem: "bff",
				Name:      "upstream_failures_total",
				Help:      "Total number of upstream Kubernetes proxy failures.",
			},
		),
		loginRateLimitBlocksTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: "servicer",
				Subsystem: "bff",
				Name:      "login_rate_limit_blocks_total",
				Help:      "Total number of login requests blocked by rate limiting.",
			},
		),
		loginRateLimitEvictionsTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: "servicer",
				Subsystem: "bff",
				Name:      "login_rate_limit_evictions_total",
				Help:      "Total number of expired login rate limiter entries evicted.",
			},
		),
	}
	registry.MustRegister(
		metrics.requestsTotal,
		metrics.requestDuration,
		metrics.authFailuresTotal,
		metrics.upstreamFailuresTotal,
		metrics.loginRateLimitBlocksTotal,
		metrics.loginRateLimitEvictionsTotal,
	)
	return metrics
}

func (m *serverMetrics) handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (s *Server) withMetrics(next http.Handler) http.Handler {
	if s.metrics == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := requestRoutePattern(r)
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(recorder, r)
		statusCode := strconv.Itoa(recorder.status)
		s.metrics.requestsTotal.WithLabelValues(r.Method, route, statusCode).Inc()
		s.metrics.requestDuration.WithLabelValues(r.Method, route, statusCode).Observe(time.Since(start).Seconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func requestRoutePattern(r *http.Request) string {
	if pattern := r.Pattern; pattern != "" {
		return pattern
	}
	if r.URL == nil || r.URL.Path == "" {
		return "unknown"
	}
	return r.URL.Path
}
