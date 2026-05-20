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
**Plan 003** — The controller can now auto-create and update Argo CD `Application` resources for a `ServiceInstance`, publish generated artifacts into a configured Git worktree path, and read Argo sync/health back into instance status. The platform now also exposes a first-class `argo-application` product contract, project-scoped repository registry endpoints, and UI flows to register Git repositories and request Argo-backed app deployments without manual secret setup in Argo CD. This is still partial: Git push/publication beyond the local worktree is not yet automated, and `ApplicationSet`-driven multi-cluster fan-out is still open.  
Commit: `e8c9ed4`

### [~] OIDC / real authentication
**Plans 005, 006** — Backend auth is no longer header-only: the BFF now supports OIDC JWT validation with issuer/client-id configuration, roles/groups claim extraction, lightweight auth/session introspection endpoints, and an explicit opt-in demo-header fallback for local/dev use. The frontend now bootstraps auth mode from the BFF, performs browser OIDC code+PKCE login, stores an authenticated browser session token, and supports sign-out. This is still partial: there is no refresh-token rotation, no server-managed cookie session, and browser logout is still provider-metadata dependent.  
Commit: `0081bad`

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
**Plans 002, 004** — `ServiceBinding` now has an API type, generated CRD, controller, and BFF list endpoint. The controller can project source `ServiceInstance` credentials into a target namespace either as a managed Secret or via External Secrets Operator resources (`ServiceAccount`, RBAC, `SecretStore`, `ExternalSecret`). Vault-backed projection and richer target integration are still open.  
Commit: `6db1e38`

### [~] Admission webhooks
**Plan 002** — `ServiceInstance`, `NamespaceClaim`, `ServiceBinding`, and `VirtualMachineClaim` now have validating/defaulting webhook handlers, first-pass webhook manifests, and an explicit `--enable-webhooks` manager flag for safe rollout. Coverage is still partial: the remaining CRDs do not yet have admission handlers, and certificate/service packaging for turnkey cluster install is still open.  
Commit: `73cdb41`

---

## Tenancy and policy

### [x] Quota enforcement
**Plan 002** — `Project.Spec.Quotas.MaxServices` and `MaxNamespaces` are checked during `ServiceInstance` reconciliation, project usage summaries are populated from current instances, and `Tenant.Spec.QuotaProfileRef` now resolves curated inherited project quotas with project-local overrides.  
Commit: `7ee64be`

### [~] Policy engine
**Plans 002, 004** — Tenant, project, and plan `PolicyRefs` are now evaluated during `ServiceInstance` reconciliation for curated platform rules such as `deny-public-ingress`, `require-external-secrets`, `require-backups`, and `protect-delete`. `ServicePlan.Spec.Constraints` are also now read for first-pass validation such as allowed exposure modes, allowed deletion policies, required secret delivery mode, replica ceilings, and backup-profile requirements. This is still partial: there is no standalone policy CRD, no CEL/OPA-style engine, and no user-defined policy bundle interpreter yet.  
Commit: `03b77eb`

---

## Delivery and secrets

### [~] Vault / external secret projection
**Plan 003** — `ServiceInstance.Spec.SecretPolicy.DeliveryMode=external-secret` now renders External Secrets Operator artifacts into the delivery tree: `ServiceAccount`, source-namespace RBAC, `SecretStore`, and `ExternalSecret` resources using the ESO Kubernetes provider. Published credential refs now point at projected target secrets rather than raw source secrets. This is still partial: Vault providers, turnkey ESO/operator packaging, and `ServiceBinding` external-secret delivery are still open.  
Commit: `3db84a0`

### [~] Sync and health status from Argo CD
**Plan 003** — Runtime observation is part of the main reconcile path, and Argo CD `Application` sync/health is ingested into `ServiceInstance.Status.Sync` when a managed `Application` exists. This remains partial because `ApplicationSet`/multi-cluster fan-out and remote Git publication are still open.  
Commit: `e8c9ed4`

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
