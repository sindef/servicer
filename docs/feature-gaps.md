# Feature Gap Analysis

Features planned across the six phase documents that are not yet implemented, or only partially so.

Status legend:
- `[ ]` Open
- `[~]` In progress / partial
- `[x]` Completed

Implementation notes in this review are current as of 2026-05-20.

---

## Critical gaps

### [~] Argo CD Application creation
**Plan 003** — The controller can now auto-create and update Argo CD `Application` resources for a `ServiceInstance`, publish generated artifacts into a configured Git worktree path, and read Argo sync/health back into instance status. This is still partial: Git push/publication beyond the local worktree is not yet automated, and `ApplicationSet`-driven multi-cluster fan-out is still open.  
Commit: `e8c9ed4`

### [~] OIDC / real authentication
**Plans 005, 006** — Backend auth is no longer header-only: the BFF now supports OIDC JWT validation with issuer/client-id configuration, roles/groups claim extraction, and an explicit opt-in demo-header fallback for local/dev use. Frontend login still falls back to demo mode and there is not yet a browser redirect/session flow.  
Commit: `7ee64be`

### [x] Approvals workflow
**Plan 006** — `ActionRequest` approval is now enforced end to end. Sensitive actions stop in `PendingApproval`, the BFF exposes an approval/rejection path for approver roles, self-approval is blocked for non-admins, and the controller refuses “approved” actions that do not carry approver identity.  
Commit: `7ee64be`

---

## Domain API

### [~] NamespaceClaim CRD
**Plan 002** — `NamespaceClaim` now has an API type, generated CRD, controller, and BFF list endpoint. It currently reconciles through a backing namespace `ServiceInstance`, which gives it real delivery behavior without a bespoke runtime path yet. A dedicated request/detail UX and richer product-specific API are still open.  
Commit: `73cdb41`

### [~] VirtualMachineClaim CRD
**Plan 002** — `VirtualMachineClaim` now has an API type, generated CRD, controller scaffold, and BFF list endpoint. It is accepted and surfaced by the control plane, but still stops in `PendingDriver` until a KubeVirt-backed runtime adapter exists.  
Commit: `73cdb41`

### [~] ServiceBinding CRD
**Plans 002, 004** — `ServiceBinding` now has an API type, generated CRD, controller, and BFF list endpoint. The controller can project source `ServiceInstance` credentials into a target namespace as a managed Secret. External secret/Vault-backed projection and richer target integration are still open.  
Commit: `73cdb41`

### [~] Admission webhooks
**Plan 002** — `ServiceInstance`, `NamespaceClaim`, `ServiceBinding`, and `VirtualMachineClaim` now have validating/defaulting webhook handlers, first-pass webhook manifests, and an explicit `--enable-webhooks` manager flag for safe rollout. Coverage is still partial: the remaining CRDs do not yet have admission handlers, and certificate/service packaging for turnkey cluster install is still open.  
Commit: `73cdb41`

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
**Plan 003** — Runtime observation is part of the main reconcile path, and Argo CD `Application` sync/health is ingested into `ServiceInstance.Status.Sync` when a managed `Application` exists. This remains partial because `ApplicationSet`/multi-cluster fan-out and remote Git publication are still open.  
Commit: `e8c9ed4`

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
