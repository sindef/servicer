package controllers

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	serviceInstanceReconcileFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "servicer",
			Subsystem: "controller",
			Name:      "serviceinstance_reconcile_failures_total",
			Help:      "Total number of ServiceInstance reconcile failures by stage.",
		},
		[]string{"stage"},
	)
	serviceInstanceActionPhasesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "servicer",
			Subsystem: "controller",
			Name:      "serviceinstance_action_phases_total",
			Help:      "Total number of ServiceInstance phase transitions observed during reconcile.",
		},
		[]string{"phase"},
	)
	actionRequestPhasesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "servicer",
			Subsystem: "controller",
			Name:      "actionrequest_phase_transitions_total",
			Help:      "Total number of ActionRequest phase transitions observed during reconcile.",
		},
		[]string{"phase"},
	)
	deliveryPublishTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "servicer",
			Subsystem: "controller",
			Name:      "delivery_publish_total",
			Help:      "Total number of delivery publish attempts by outcome.",
		},
		[]string{"status"},
	)
	serviceInstanceReconcileDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "servicer",
			Subsystem: "controller",
			Name:      "serviceinstance_reconcile_duration_seconds",
			Help:      "Duration of ServiceInstance reconcile executions.",
			Buckets:   prometheus.DefBuckets,
		},
	)
)

func init() {
	metrics.Registry.MustRegister(
		serviceInstanceReconcileFailuresTotal,
		serviceInstanceActionPhasesTotal,
		actionRequestPhasesTotal,
		deliveryPublishTotal,
		serviceInstanceReconcileDuration,
	)
}

func observeServiceInstancePhase(phase string) {
	if phase == "" {
		phase = "unknown"
	}
	serviceInstanceActionPhasesTotal.WithLabelValues(phase).Inc()
}

func observeActionRequestPhase(phase string) {
	if phase == "" {
		phase = "unknown"
	}
	actionRequestPhasesTotal.WithLabelValues(phase).Inc()
}

func observeServiceInstanceFailure(stage string) {
	if stage == "" {
		stage = "unknown"
	}
	serviceInstanceReconcileFailuresTotal.WithLabelValues(stage).Inc()
}

func observeDeliveryPublish(status string) {
	if status == "" {
		status = "unknown"
	}
	deliveryPublishTotal.WithLabelValues(status).Inc()
}

func observeServiceInstanceReconcileDuration(start time.Time) {
	serviceInstanceReconcileDuration.Observe(time.Since(start).Seconds())
}
