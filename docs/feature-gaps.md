# Feature Gap Analysis

Features planned across the six phase documents that are not yet implemented, or only partially so.

Status legend:
- `[ ]` Open
- `[~]` In progress / partial
- `[x]` Completed

Implementation notes in this review are current as of 2026-05-20.

---

## Critical gaps

### [ ] Argo CD Application creation
**Plan 003** — The materializer writes YAML artifacts to `generated/delivery/` on the local filesystem. In the demo environment a polling shell script (`hack/demo-sync-delivery.sh`) runs `kubectl apply -f` against those directories every 3 seconds, which is what drives the visible "sync". No `Application` or `ApplicationSet` object is ever created; no Argo CD API types are imported. The plan calls for a proper Argo CD delivery strategy — auto-created Applications, sync status reflected back into `ServiceInstance.Status.Sync`, and ApplicationSet-based multi-cluster fan-out — none of which is implemented. The current approach is a local dev shortcut that cannot work across real target clusters.

### [~] OIDC / real authentication
**Plans 005, 006** — Backend auth is no longer header-only: the BFF now supports OIDC JWT validation with issuer/client-id configuration, roles/groups claim extraction, and an explicit opt-in demo-header fallback for local/dev use. Frontend login still falls back to demo mode and there is not yet a browser redirect/session flow.  
Commit: `7ee64be`

### [x] Approvals workflow
**Plan 006** — `ActionRequest` approval is now enforced end to end. Sensitive actions stop in `PendingApproval`, the BFF exposes an approval/rejection path for approver roles, self-approval is blocked for non-admins, and the controller refuses “approved” actions that do not carry approver identity.  
Commit: `7ee64be`

---

## Domain API

### [ ] NamespaceClaim CRD
**Plan 002** — Mentioned as a first-class resource alongside `ServiceInstance`. No type definition exists in `api/v1alpha1/`.

### [ ] VirtualMachineClaim CRD
**Plan 002** — Mentioned as a planned resource for KubeVirt-backed VMs. No type definition exists.

### [ ] ServiceBinding CRD
**Plans 002, 004** — Intended for secure credential sharing between service instances. No type definition, no controller, no BFF endpoint.

### [ ] Admission webhooks
**Plan 002** — Validation and defaulting webhooks are planned for all CRDs. CRD types have `kubebuilder` marker annotations for static schema validation only. No `ValidatingWebhookConfiguration`, no `MutatingWebhookConfiguration`, and no admission handler code exist.

---

## Tenancy and policy

### [~] Quota enforcement
**Plan 002** — `Project.Spec.Quotas.MaxServices` and `MaxNamespaces` are now checked during `ServiceInstance` reconciliation, and project usage summaries are populated from current instances. `Tenant.Spec.QuotaProfileRef` still is not resolved into inherited quotas, so quota-profile-driven enforcement remains open.  
Commit: `7ee64be`

### [ ] Policy engine
**Plans 002, 004** — `Tenant.Spec.PolicyRefs` and `Project.Spec.PolicyRefs` are defined but there is no policy evaluation engine. `ServicePlan.Spec.PolicyRefs` and `Constraints` fields also go unread.

---

## Delivery and secrets

### [ ] Vault / external secret projection
**Plan 003** — `ServiceInstance.Spec.SecretPolicy.DeliveryMode` has an `external-secret` enum value but no handler. All credentials are written as plain Kubernetes Secrets. No Vault client, no External Secrets Operator integration.

### [~] Sync and health status from Argo CD
**Plan 003** — Runtime observation is now part of the main reconcile path and adapters’ `Observe()` methods are called, so runtime-ready state is not materialize-only anymore. Actual Argo CD `Application` sync/health state still is not read, because no Argo `Application` objects are created yet.  
Commit: `7ee64be`

---

## Product catalog

### [ ] K8ssandra / Cassandra adapter
**Plan 004** — `k8ssandra.go` defines a `ProductContract` (topology modes, health signals, actions) but does not implement the `ServiceAdapter` interface. The `Validate`, `Render`, `Observe`, `Delete`, and `ExecuteAction` methods are absent. The adapter is not registered in the adapter registry or instantiated in `cmd/manager`.

### [ ] KubeVirt adapter
**Plans 004 stretch, 006** — No adapter code, no contract definition, no API types imported. Mentioned only in plan docs and the README roadmap.

### [~] Backup config wiring — PostgreSQL and MySQL
**Plan 004** — PostgreSQL/CNPG backup object-store wiring is implemented and rendered into CNPG backup manifests. MySQL still only consumes the coarse `backupProfile` contract and does not yet materialize a real backup execution/storage pipeline.  
Commit: `7ee64be`

### [ ] NATS geo multi-cluster
**Plan 004** — The `nats-geo` plan topology is defined and the UI guards JetStream-specific UI behind the correct plan. The adapter renders single-cluster JetStream only; standby cluster parameters are not processed and no multi-cluster replication is configured.

### [ ] YugabyteDB xCluster replication
**Plan 004** — The Yugabyte adapter detects `multi-region` topology and renders a `YBUniverse` per cluster, but does not configure xCluster replication between them. The `replication-lag` health signal is declared in the contract but never populated.

---

## Observability and operations

### [~] Audit retention and structured search
**Plan 006** — `/api/audit` now supports structured filters (`type`, `actor`, `resource`, `action`, `phase`, `from`, `to`, `limit`) in addition to substring search. Durable retention is still open: Kubernetes Events still age out and there is not yet an audit-specific persistence store or retention policy.  
Commit: `7ee64be`

### [ ] Control-plane backup and restore
**Plan 006** — No etcd backup, no CRD export/import, and no control-plane snapshot tooling. Database service adapters support a `backup` action, but Servicer itself is not backed up.

### [ ] Metrics, SLOs, dashboards, and alerting
**Plan 006** — `cmd/manager` exposes controller-runtime metrics, but there is still no meaningful Servicer-specific metrics surface, BFF Prometheus instrumentation, dashboard pack, alert rules, or SLO layer. This remains operationally open.
