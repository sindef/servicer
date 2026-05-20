# Feature Gap Analysis

Features planned across the six phase documents that are not yet implemented, or only partially so.

---

## Critical gaps

### Argo CD Application creation
**Plan 003** — The materializer writes YAML artifacts to `generated/delivery/` on the local filesystem. In the demo environment a polling shell script (`hack/demo-sync-delivery.sh`) runs `kubectl apply -f` against those directories every 3 seconds, which is what drives the visible "sync". No `Application` or `ApplicationSet` object is ever created; no Argo CD API types are imported. The plan calls for a proper Argo CD delivery strategy — auto-created Applications, sync status reflected back into `ServiceInstance.Status.Sync`, and ApplicationSet-based multi-cluster fan-out — none of which is implemented. The current approach is a local dev shortcut that cannot work across real target clusters.

### OIDC / real authentication
**Plans 005, 006** — Auth is demo-only. The BFF reads `X-Servicer-User`, `X-Servicer-Roles`, and `X-Servicer-Groups` request headers. There is no JWT validation, no OIDC provider integration, and no group/team mapping. The frontend falls back to `localStorage` for demo mode.

### Approvals workflow
**Plan 006** — `ActionRequest.Spec.Approval` and the `ApprovalMode` enum (`auto`, `required`, `approved`, `rejected`) are defined, and adapter contracts mark actions as `RequiresApproval: true`, but the `ActionRequestReconciler` does not check any of this. Actions proceed directly to execution with no approval gate, no `PendingApproval` phase, and no approver role checks.

---

## Domain API

### NamespaceClaim CRD
**Plan 002** — Mentioned as a first-class resource alongside `ServiceInstance`. No type definition exists in `api/v1alpha1/`.

### VirtualMachineClaim CRD
**Plan 002** — Mentioned as a planned resource for KubeVirt-backed VMs. No type definition exists.

### ServiceBinding CRD
**Plans 002, 004** — Intended for secure credential sharing between service instances. No type definition, no controller, no BFF endpoint.

### Admission webhooks
**Plan 002** — Validation and defaulting webhooks are planned for all CRDs. CRD types have `kubebuilder` marker annotations for static schema validation only. No `ValidatingWebhookConfiguration`, no `MutatingWebhookConfiguration`, and no admission handler code exist.

---

## Tenancy and policy

### Quota enforcement
**Plan 002** — `Project.Spec.Quotas.MaxServices` and `MaxNamespaces` fields are defined. `Tenant.Spec.QuotaProfileRef` exists. Neither is enforced by any controller. The `ServiceInstanceReconciler` checks `Tenant.Spec.AllowedServiceClasses` but performs no quota counting.

### Policy engine
**Plans 002, 004** — `Tenant.Spec.PolicyRefs` and `Project.Spec.PolicyRefs` are defined but there is no policy evaluation engine. `ServicePlan.Spec.PolicyRefs` and `Constraints` fields also go unread.

---

## Delivery and secrets

### Vault / external secret projection
**Plan 003** — `ServiceInstance.Spec.SecretPolicy.DeliveryMode` has an `external-secret` enum value but no handler. All credentials are written as plain Kubernetes Secrets. No Vault client, no External Secrets Operator integration.

### Sync and health status from Argo CD
**Plan 003** — `ServiceInstance.Status.Sync` and `.Status.Health` types exist and the reconciler sets `Sync.Phase = "Materialized"` after rendering. The reconciler does not read actual Argo Application sync state, so phase stays at `Materialized` indefinitely once rendered. The adapter `Observe()` method is also not called in the main reconcile path.

---

## Product catalog

### K8ssandra / Cassandra adapter
**Plan 004** — `k8ssandra.go` defines a `ProductContract` (topology modes, health signals, actions) but does not implement the `ServiceAdapter` interface. The `Validate`, `Render`, `Observe`, `Delete`, and `ExecuteAction` methods are absent. The adapter is not registered in the adapter registry or instantiated in `cmd/manager`.

### KubeVirt adapter
**Plans 004 stretch, 006** — No adapter code, no contract definition, no API types imported. Mentioned only in plan docs and the README roadmap.

### Backup config wiring — PostgreSQL and MySQL
**Plan 004** — `BackupProfile`, `BackupEndpoint`, `BackupBucket`, and `BackupRegion` parameters are captured in the frontend form and stored on the `ServiceInstance`, but the CNPG and MySQL adapters do not read or materialize them.

### NATS geo multi-cluster
**Plan 004** — The `nats-geo` plan topology is defined and the UI guards JetStream-specific UI behind the correct plan. The adapter renders single-cluster JetStream only; standby cluster parameters are not processed and no multi-cluster replication is configured.

### YugabyteDB xCluster replication
**Plan 004** — The Yugabyte adapter detects `multi-region` topology and renders a `YBUniverse` per cluster, but does not configure xCluster replication between them. The `replication-lag` health signal is declared in the contract but never populated.

---

## Observability and operations

### Audit retention and structured search
**Plan 006** — The `/api/audit` endpoint aggregates `ActionRequest` objects and Kubernetes Events with a basic substring search. There is no durable storage backend; Kubernetes Events have a ~1 hour default TTL. No structured query options (date range, actor, resource type), no retention policy, and no audit-specific store.

### Control-plane backup and restore
**Plan 006** — No etcd backup, no CRD export/import, and no control-plane snapshot tooling. Database service adapters support a `backup` action, but Servicer itself is not backed up.

### Metrics, SLOs, dashboards, and alerting
**Plan 006** — No Prometheus metrics are emitted from `cmd/manager` or `cmd/bff`. No dashboard definitions, no alerting rules, no SLO instrumentation.
