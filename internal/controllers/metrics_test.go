package controllers

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestControllerMetricsObserveHelpers(t *testing.T) {
	failuresBefore := testutil.ToFloat64(serviceInstanceReconcileFailuresTotal.WithLabelValues("unit_test"))
	observeServiceInstanceFailure("unit_test")
	failuresAfter := testutil.ToFloat64(serviceInstanceReconcileFailuresTotal.WithLabelValues("unit_test"))
	if failuresAfter != failuresBefore+1 {
		t.Fatalf("expected reconcile failure counter increment, before=%v after=%v", failuresBefore, failuresAfter)
	}

	phasesBefore := testutil.ToFloat64(serviceInstanceActionPhasesTotal.WithLabelValues("Ready"))
	observeServiceInstancePhase("Ready")
	phasesAfter := testutil.ToFloat64(serviceInstanceActionPhasesTotal.WithLabelValues("Ready"))
	if phasesAfter != phasesBefore+1 {
		t.Fatalf("expected phase counter increment, before=%v after=%v", phasesBefore, phasesAfter)
	}

	actionPhasesBefore := testutil.ToFloat64(actionRequestPhasesTotal.WithLabelValues("Succeeded"))
	observeActionRequestPhase("Succeeded")
	actionPhasesAfter := testutil.ToFloat64(actionRequestPhasesTotal.WithLabelValues("Succeeded"))
	if actionPhasesAfter != actionPhasesBefore+1 {
		t.Fatalf("expected action request phase counter increment, before=%v after=%v", actionPhasesBefore, actionPhasesAfter)
	}

	publishBefore := testutil.ToFloat64(deliveryPublishTotal.WithLabelValues("succeeded"))
	observeDeliveryPublish("succeeded")
	publishAfter := testutil.ToFloat64(deliveryPublishTotal.WithLabelValues("succeeded"))
	if publishAfter != publishBefore+1 {
		t.Fatalf("expected publish counter increment, before=%v after=%v", publishBefore, publishAfter)
	}
}

func TestControllerMetricsObserveDuration(t *testing.T) {
	countBefore := testutil.CollectAndCount(serviceInstanceReconcileDuration)
	observeServiceInstanceReconcileDuration(time.Now().Add(-150 * time.Millisecond))
	countAfter := testutil.CollectAndCount(serviceInstanceReconcileDuration)
	if countAfter < countBefore {
		t.Fatalf("expected histogram collector samples not to decrease, before=%d after=%d", countBefore, countAfter)
	}
}
