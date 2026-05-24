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

Alerts cover controller reconcile failures, delivery publish failures, auth failures, backup failures, Argo sync drift, and degraded product health. Custom resource condition alerts require kube-state-metrics custom resource state metrics for Servicer CRDs.
