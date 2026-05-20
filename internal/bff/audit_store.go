package bff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultAuditNamespace     = "servicer-system"
	defaultAuditRetentionDays = 90
	auditEventLabelKey        = "servicer.io/audit-event"
	auditEventLabelValue      = "true"
)

type auditStore struct {
	client        client.Client
	namespace     string
	retentionDays int
}

func newAuditStoreFromEnv(kubeClient client.Client) *auditStore {
	namespace := os.Getenv("SERVICER_AUDIT_NAMESPACE")
	if namespace == "" {
		namespace = defaultAuditNamespace
	}
	retentionDays := defaultAuditRetentionDays
	if raw := os.Getenv("SERVICER_AUDIT_RETENTION_DAYS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			retentionDays = parsed
		}
	}
	return &auditStore{client: kubeClient, namespace: namespace, retentionDays: retentionDays}
}

func (s *auditStore) merge(ctx context.Context, events []AuditEventSummary) ([]AuditEventSummary, error) {
	if s == nil || s.client == nil {
		return events, nil
	}
	retained, err := s.retained(ctx)
	if err != nil {
		return nil, err
	}
	merged := mergeAuditEvents(retained, events)
	if err := s.persist(ctx, events); err != nil {
		return nil, err
	}
	return merged, nil
}

func (s *auditStore) retained(ctx context.Context) ([]AuditEventSummary, error) {
	var configMaps corev1.ConfigMapList
	if err := s.client.List(ctx, &configMaps, client.InNamespace(s.namespace), client.MatchingLabels{auditEventLabelKey: auditEventLabelValue}); err != nil {
		return nil, err
	}
	cutoff := time.Now().AddDate(0, 0, -s.retentionDays)
	events := make([]AuditEventSummary, 0, len(configMaps.Items))
	for _, configMap := range configMaps.Items {
		var event AuditEventSummary
		if err := json.Unmarshal([]byte(configMap.Data["event.json"]), &event); err != nil {
			continue
		}
		if auditEventBefore(event, cutoff) {
			stale := configMap
			_ = s.client.Delete(ctx, &stale)
			continue
		}
		events = append(events, event)
	}
	return events, nil
}

func (s *auditStore) persist(ctx context.Context, events []AuditEventSummary) error {
	cutoff := time.Now().AddDate(0, 0, -s.retentionDays)
	for _, event := range events {
		if auditEventBefore(event, cutoff) {
			continue
		}
		payload, err := json.Marshal(event)
		if err != nil {
			return err
		}
		name := auditEventConfigMapName(payload)
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: s.namespace,
				Labels: map[string]string{
					auditEventLabelKey:            auditEventLabelValue,
					"app.kubernetes.io/name":      "servicer",
					"app.kubernetes.io/component": "audit",
				},
			},
			Data: map[string]string{"event.json": string(payload)},
		}
		if err := s.client.Create(ctx, configMap); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	}
	return nil
}

func auditEventConfigMapName(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "audit-" + hex.EncodeToString(sum[:])[:20]
}

func auditEventBefore(event AuditEventSummary, cutoff time.Time) bool {
	if event.Time == "" {
		return false
	}
	eventTime, err := time.Parse(time.RFC3339, event.Time)
	if err != nil {
		return false
	}
	return eventTime.Before(cutoff)
}

func mergeAuditEvents(left, right []AuditEventSummary) []AuditEventSummary {
	seen := map[string]struct{}{}
	merged := make([]AuditEventSummary, 0, len(left)+len(right))
	for _, event := range append(left, right...) {
		key := auditEventKey(event)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, event)
	}
	return merged
}

func auditEventKey(event AuditEventSummary) string {
	payload, _ := json.Marshal(event)
	return string(payload)
}
