# Observability

Install Prometheus Operator resources with:

```bash
kubectl apply -k config/observability
```

The overlay includes:

- `ServiceMonitor` for `manager-metrics`
- `ServiceMonitor` for BFF metrics on port `9090`
- Prometheus SLO and operations alerts
- Grafana dashboard ConfigMap labeled with `grafana_dashboard=1`

Required scrape targets:

- `servicer-system/manager-metrics:8080`
- `servicer-system/bff:9090`

Alerts cover controller reconcile failures, ServiceInstance reconcile failures, delivery publish failures, repository mirror failures, audit persistence failures, auth failures, login rate-limit spikes, backup failures, Argo sync drift, and degraded product health. Custom resource condition alerts require kube-state-metrics custom resource state metrics for Servicer CRDs.

Every BFF response includes `X-Request-Id` and `X-Correlation-Id`. BFF error logs and persisted BFF audit records include the request ID when available, which is the primary correlation key from user-visible API failures to audit records and controller-created resources.
