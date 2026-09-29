package bff

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type failingDeleteClient struct {
	client.Client
	err error
}

func (c *failingDeleteClient) Delete(_ context.Context, _ client.Object, _ ...client.DeleteOption) error {
	return c.err
}

func expiredAuditConfigMap() *corev1.ConfigMap {
	event := fmt.Sprintf(
		`{"time":%q,"type":"login","subject":"alice@example.com","requestId":"req-1"}`,
		time.Now().AddDate(0, 0, -365).Format(time.RFC3339),
	)
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "audit-expired",
			Namespace: "servicer-system",
			Labels: map[string]string{
				auditEventLabelKey: auditEventLabelValue,
			},
		},
		Data: map[string]string{"event.json": event},
	}
}

func auditTestClient(t *testing.T) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(expiredAuditConfigMap()).Build()
}

func TestRetainedKeepsEventsRemovableWhenExpiredDeleteFails(t *testing.T) {
	store := &auditStore{
		client:        &failingDeleteClient{Client: auditTestClient(t), err: fmt.Errorf("delete denied")},
		namespace:     "servicer-system",
		retentionDays: 90,
	}

	events, err := store.retained(context.Background())

	if err != nil {
		t.Fatalf("retained returned error: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected expired event to be filtered, got %d", len(events))
	}
}

type capturingHandler struct {
	records []slog.Record
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(string) slog.Handler      { return h }

func TestRetainedLogsWhenExpiredConfigMapDeleteFails(t *testing.T) {
	handler := &capturingHandler{}
	previous := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(previous)

	store := &auditStore{
		client:        &failingDeleteClient{Client: auditTestClient(t), err: fmt.Errorf("delete denied")},
		namespace:     "servicer-system",
		retentionDays: 90,
	}

	_, err := store.retained(context.Background())
	if err != nil {
		t.Fatalf("retained returned error: %v", err)
	}
	if len(handler.records) != 1 {
		t.Fatalf("expected one log record, got %d", len(handler.records))
	}
	found := false
	handler.records[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "name" && a.Value.String() == "audit-expired" {
			found = true
		}
		return true
	})
	if !found {
		t.Fatalf("expected log record to include ConfigMap name, got %#v", handler.records[0])
	}
	if handler.records[0].Level != slog.LevelError {
		t.Fatalf("expected error level log, got %v", handler.records[0].Level)
	}
}

func TestRetainedDeletesExpiredAuditConfigMaps(t *testing.T) {
	base := auditTestClient(t)
	store := &auditStore{client: base, namespace: "servicer-system", retentionDays: 90}

	events, err := store.retained(context.Background())
	if err != nil {
		t.Fatalf("retained returned error: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected expired event to be filtered, got %d", len(events))
	}

	var remaining corev1.ConfigMapList
	if err := base.List(context.Background(), &remaining, client.InNamespace("servicer-system")); err != nil {
		t.Fatalf("list returned error: %v", err)
	}
	if len(remaining.Items) != 0 {
		t.Fatalf("expected expired ConfigMap to be deleted, found %d", len(remaining.Items))
	}
}
