# Feature Gap Analysis

Features planned across the six phase documents that are not yet implemented, or only partially so.

Status legend:
- `[ ]` Open
- `[~]` In progress / partial
- `[x]` Completed

Implementation notes in this review are current as of 2026-05-21.

---

## Critical gaps

### [x] Argo CD Application creation
**Plan 003** — The controller now auto-creates and updates Argo CD `Application` resources for single-cluster deliveries, pushes generated artifacts to a configured remote Git repository after local publication, and switches to Argo CD `ApplicationSet` fan-out when adapters render multiple cluster package paths. The platform continues to expose a first-class `argo-application` product contract, project-scoped repository registry endpoints, and UI flows to register Git repositories and request Argo-backed app deployments without manual secret setup in Argo CD.  
Commit: `4ae28c8`

### [x] OIDC / real authentication
**Plans 005, 006** — Backend auth is no longer header-only: the BFF now supports OIDC JWT validation with issuer/client-id configuration, roles/groups claim extraction, encrypted server-managed browser session cookies, refresh-token renewal for browser sessions, explicit login/callback/logout endpoints, and an opt-in demo-header fallback for local/dev use. The frontend now bootstraps auth mode from the BFF, relies on the server-managed browser session rather than local token storage, and supports browser sign-in/sign-out through the BFF flow.  
Commit: `76c9a05`

### [x] Approvals workflow
**Plan 006** — `ActionRequest` approval is now enforced end to end. Sensitive actions stop in `PendingApproval`, the BFF exposes an approval/rejection path for approver roles, self-approval is blocked for non-admins, and the controller refuses “approved” actions that do not carry approver identity.  
Commit: `7ee64be`

---

## Domain API

### [x] NamespaceClaim CRD
**Plan 002** — `NamespaceClaim` now has an API type, generated CRD, controller, dedicated BFF list/detail/write endpoints, and first-class web UX for requesting, inspecting, editing, and deleting namespace claims. It still reconciles through a backing namespace `ServiceInstance`, but the product-specific API and request/detail experience are now in place.  
Commit: `260deb3`

### [x] VirtualMachineClaim CRD
**Plan 002** — `VirtualMachineClaim` now has an API type, generated CRD, webhook coverage, BFF list endpoint, and controller path that creates a backing `virtual-machine` `ServiceInstance`, maps image and power-state intent into KubeVirt parameters, and mirrors runtime/artifact/sync/health status back to the claim.
Commit: `23298fb`

### [~] ServiceBinding CRD
**Plans 002, 004** — `ServiceBinding` now has an API type, generated CRD, controller, and BFF list endpoint. The controller can project source `ServiceInstance` credentials into a target namespace either as a managed Secret, via External Secrets Operator resources using the Kubernetes provider, or via Vault-backed ESO `SecretStore`/`ExternalSecret` delivery. The remaining open area is richer target integration beyond secret projection itself.  
Commit: `4059a83`

### [x] Admission webhooks
**Plan 002** — All current Servicer CRDs now have admission webhook coverage, including validating/defaulting handlers for `ActionRequest`, `ClusterTarget`, `OperatorPackage`, `Policy`, `Project`, `ServiceClass`, `ServiceInstance`, `ServicePlan`, `ServiceBinding`, `Tenant`, `NamespaceClaim`, and `VirtualMachineClaim`. Turnkey cluster install is also wired now via deployed webhook configurations, dedicated webhook Service, manager webhook TLS mount, and bootstrap Job logic that generates serving certificates and patches webhook `caBundle` values in-cluster.  
Commit: `4f9093c`

---

## Tenancy and policy

### [x] Quota enforcement
**Plan 002** — `Project.Spec.Quotas.MaxServices` and `MaxNamespaces` are checked during `ServiceInstance` reconciliation, project usage summaries are populated from current instances, and `Tenant.Spec.QuotaProfileRef` now resolves curated inherited project quotas with project-local overrides.  
Commit: `7ee64be`

### [x] Policy engine
**Plans 002, 004** — Tenant, project, and plan `PolicyRefs` are now evaluated during `ServiceInstance` reconciliation for both curated platform rules and standalone cluster-scoped `Policy` objects. User-defined policies can now declare rule bundles against normalized instance fields such as `spec.exposure.mode`, `spec.secretPolicy.deliveryMode`, `spec.deletionPolicy`, and `parameters.*`, with deterministic operators for equality, membership, presence, emptiness, and numeric comparison. `ServicePlan.Spec.Constraints` continue to provide first-pass plan validation for allowed exposure modes, allowed deletion policies, required secret delivery mode, replica ceilings, and backup-profile requirements.  
Commit: `85f267b`

---

## Delivery and secrets

### [~] Vault / external secret projection
**Plan 003** — `ServiceInstance.Spec.SecretPolicy.DeliveryMode=external-secret` now renders External Secrets Operator artifacts into the delivery tree for both Kubernetes-provider and Vault-provider delivery modes, and published credential refs point at projected target secrets rather than raw source secrets. `ServiceBinding` now also supports Vault-backed external-secret delivery into its target namespace. This remains partial only because turnkey ESO/operator packaging is tracked separately.  
Commit: `4059a83`

### [x] Sync and health status from Argo CD
**Plan 003** — Runtime observation is part of the main reconcile path, Argo CD `Application` sync/health is ingested into `ServiceInstance.Status.Sync` when a managed `Application` exists, and multi-cluster deliveries now aggregate sync status from `ApplicationSet`-generated child applications. Remote Git publication is also automated as part of the delivery handoff.  
Commit: `4ae28c8`

---

## Product catalog

### [~] K8ssandra / Cassandra adapter
**Plan 004** — Cassandra now has a first-pass K8ssandra-backed `ServiceAdapter`, sample catalog entries, requestable catalog exposure, and deterministic manifest rendering for single- and multi-datacenter topologies. This is still partial: action execution is queued/placeholder only, runtime observation is workload-level rather than operator-deep, and richer backup/repair orchestration remains open.

### [~] KubeVirt adapter
**Plans 004 stretch, 006** — Servicer now has a first-pass `virtual-machine` product contract, KubeVirt-backed adapter, sample catalog entries, deterministic rendering for a namespace, cloud-init Secret, persistent CDI `DataVolume` root disk, and KubeVirt `VirtualMachine`, and `VirtualMachineClaim` routes through a backing adapter-managed `ServiceInstance`. This is still partial: runtime observation is shallow.

### [x] Backup config wiring — PostgreSQL and MySQL
**Plan 004** — PostgreSQL/CNPG backup object-store wiring is implemented and rendered into CNPG backup manifests. MySQL now materializes `backupProfile` into a backup PVC and scheduled `mysqldump` `CronJob` with profile-derived schedule and retention metadata.  
Commit: `7ee64be`

### [x] NATS geo multi-cluster
**Plan 004** — The `nats-geo` plan validates standby cluster input, renders per-cluster package paths, configures StatefulSet clustering via explicit route lists, emits inter-cluster gateway services plus gateway peer config for JetStream federation, and reports `gateway-health` from standby replication lag thresholds.

### [x] YugabyteDB xCluster replication
**Plan 004** — Multi-region YugabyteDB now renders a primary `YBUniverse`, standby `YBUniverse` resources, an operator-side xCluster setup `Job`, and a populated `replication-lag` health signal from reported standby lag thresholds.

---

## Observability and operations

### [x] Audit retention and structured search
**Plan 006** — `/api/audit` supports structured filters (`type`, `actor`, `resource`, `action`, `phase`, `from`, `to`, `limit`) in addition to substring search, and the BFF now mirrors audit summaries into a labelled ConfigMap-backed audit store with `SERVICER_AUDIT_NAMESPACE` and `SERVICER_AUDIT_RETENTION_DAYS` retention controls.  
Commit: `7ee64be`

### [x] Control-plane backup and restore
**Plan 006** — Best-effort control-plane snapshot tooling exists via `hack/control-plane-backup.sh`, and `config/backup` now packages RBAC, a backup PVC, scheduled backup `CronJob`, and restore `Job` template for Servicer CRDs, `servicer-system` runtime state, Servicer-managed Argo CD Applications, and generated delivery artifacts.  
Commit: `6db1e38`

### [x] Metrics, SLOs, dashboards, and alerting
**Plan 006** — `cmd/manager` exposes controller-runtime metrics, the BFF exposes Prometheus request/auth/upstream metrics, and `config/observability` now includes Prometheus SLO recording/alert rules plus a Grafana dashboard pack for BFF availability, latency, auth failures, upstream failures, and controller reconcile health.  
Commit: `a714a01`
