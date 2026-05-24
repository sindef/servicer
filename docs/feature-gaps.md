# Production Feature Gaps

Last reviewed: 2026-05-24

This document tracks the gaps that still need closing before Servicer should be
treated as production-ready. It is based on the current repository state, not on
the original phase plan checklist.

## Read This First

Servicer has a real foundation now: CRDs, reconcilers, BFF APIs, an admin UI,
auth resources, deployment manifests, CI builds, GHCR publishing, and generated
delivery artifacts. The remaining work is less about proving the concept and
more about making the control plane safe, operable, secure, recoverable, and
boring in real customer clusters.

Priority meanings:

- `P0` blocks production use.
- `P1` should be completed before a GA release.
- `P2` is hardening or scale maturity that can follow once P0/P1 are owned.

## P0 Production Blockers

### Delivery path is not production GitOps by default

The controller can publish delivery artifacts, but the production manifest still
uses an ephemeral local delivery path unless operators manually wire the GitOps
pieces.

Evidence in repo:

- `deploy/manager.yaml` mounts `/delivery` as `emptyDir`.
- The syncer sidecar reads from `/delivery` and can use a local kubeconfig,
  which is useful for local validation but not a strong production contract.
- `cmd/manager/main.go` supports delivery repo URL, worktree, auto-commit, and
  auto-push flags, but `deploy/manager.yaml` does not configure them.
- Generated artifacts can be lost on manager pod restart if no external Git
  worktree or persistent volume is configured.

Required before production:

- Define one supported production delivery mode: GitOps repository first.
- Add manifests for repo credentials, known-hosts/CA material, branch/ref,
  delivery path, commit identity, and optional auto-push.
- Decide whether the syncer sidecar is demo-only or production-supported. If it
  is production-supported, document its security model and lifecycle.
- Add reconciliation behavior for failed Git push, divergent worktrees, and
  unavailable remote repositories.
- Add e2e coverage proving a ServiceInstance creates committed artifacts and an
  Argo CD Application syncs them.

### Backup and restore coverage is incomplete

Backup scaffolding exists, but it is not yet a production disaster-recovery
story.

Evidence in repo:

- `config/backup` is not included by `deploy/kustomization.yaml`.
- Backup and restore Jobs reference `ghcr.io/sindef/servicer-tools:latest`, but
  the CI workflow does not build or publish a `servicer-tools` image.
- `hack/control-plane-backup.sh` describes the snapshot as best-effort, not a
  full etcd backup.
- The backup resource list does not cover all newer control-plane APIs, such as
  auth providers, users, groups, role bindings, operator packages, and policies.
- Delivery artifacts are captured only from the configured delivery volume/path,
  which is unsafe if production delivery is meant to be Git-backed.

Required before production:

- Build and publish a versioned `servicer-tools` image.
- Include backup/restore manifests in the production install or a documented
  production add-on.
- Update backup RBAC and scripts to include every Servicer CRD and runtime
  namespace resource needed for restore.
- Add restore validation in CI or nightly e2e: install, create tenants/projects,
  create services, back up, restore into a fresh cluster, and verify status.
- Document what is and is not covered compared with an etcd-level cluster
  backup.

### Product catalog and operator lifecycle are incomplete

Servicer can model products, but production users need a supported catalog
lifecycle, not just sample manifests.

Evidence in repo:

- `deploy/operator-packages.yaml` seeds External Secrets Operator only.
- Product examples live mostly under samples/config paths.
- Some sample operator/package data contains demo defaults that should not be
  copied into production.
- The admin UI can register known service classes, but plan/package lifecycle is
  not yet a complete production management path.

Required before production:

- Define the supported production catalog set and version policy.
- Seed required operator packages for every product advertised as production
  supported.
- Add upgrade, rollback, and compatibility guidance for operator packages.
- Move demo-only defaults out of production paths.
- Add UI/API support for the full catalog lifecycle or clearly document that it
  is GitOps-managed.

## P1 GA Gaps

### Observability is present but not packaged as production monitoring

Metrics, rules, and dashboards exist, but production install does not wire them
up end to end.

Evidence in repo:

- `config/observability` contains Prometheus rules and a Grafana dashboard.
- `deploy/kustomization.yaml` does not include observability resources.
- `deploy/manager.yaml` disables manager metrics with
  `--metrics-bind-address=:0`.
- There is no `ServiceMonitor`, `PodMonitor`, or scrape documentation in the
  production install.

Required before GA:

- Enable manager metrics by default or via a clear production overlay.
- Add ServiceMonitor/PodMonitor resources for Prometheus Operator users.
- Include alert rules and dashboards as an installable overlay.
- Add alerts for reconcile failures, failed delivery publication, failed auth,
  backup failure, Argo sync drift, and degraded product health.

### Audit trail is not yet compliance-grade

Audit functionality exists, but it is not durable or complete enough for serious
production accountability.

Evidence in repo:

- Audit data is derived from ActionRequests, Kubernetes Events, and BFF-side
  summaries rather than a first-class immutable audit sink.
- ConfigMap-backed audit storage is useful for demos but not a durable,
  tamper-resistant audit log.
- CRUD operations across tenants, projects, auth resources, catalog objects, and
  repository configuration need consistent audit records.
- The `roleAuditor` concept exists, but audit endpoint authorization should be
  checked for exact intended role behavior.

Required before GA:

- Emit structured audit events for all login, logout, auth changes, CRUD,
  approval, secret access, delivery, and admin actions.
- Support an external audit sink such as stdout JSON, webhook, object storage,
  or SIEM-forwarder integration.
- Make retention, filtering, and tenant scoping explicit.
- Add tests for audit coverage on every privileged endpoint.

### Product adapter action maturity is uneven

The adapter layer is broad, but not all advertised day-2 actions are actually
implemented with production-grade behavior.

Evidence in repo:

- Some adapters advertise or accept actions that still return queued or
  placeholder results.
- Cassandra/K8ssandra support is still first-pass and lacks operator-deep
  repair, backup, and health behavior.
- KubeVirt support is still shallow around guest readiness, storage conditions,
  and power/action execution.
- MySQL backup/restore and more advanced topologies need stronger runtime
  validation.
- NATS, Valkey, Yugabyte, and PostgreSQL need repeatable failover/restore/drift
  drills against real operators, not only manifest rendering tests.

Required before GA:

- Publish an action support matrix per product and plan.
- Hide or block unsupported actions instead of returning placeholder queued
  states.
- Add integration tests against real operators for every product marked
  production-supported.
- Define health semantics per product: ready, degraded, failed, progressing,
  backup stale, replication lag, and restore required.

### End-to-end and upgrade testing is missing

Unit coverage is useful, and CI builds the app, but production readiness needs
cluster-level validation.

Evidence in repo:

- CI runs Go tests, frontend build, npm audit, and kustomize rendering.
- There is no obvious e2e suite that installs Servicer into a KinD cluster with
  Argo CD and real product operators.
- There are no CRD upgrade/conversion tests or stored-object migration tests.

Required before GA:

- Add a KinD-based e2e suite for install, login, tenant/project creation,
  catalog setup, service provisioning, Argo sync, action approval, backup, and
  restore.
- Add upgrade tests from the previous released version to the current version.
- Add CRD schema compatibility tests using stored object fixtures.
- Add manifest policy tests for probes, resources, security contexts, and RBAC.

### API versioning and compatibility policy are absent

All APIs are still `platform.servicer.io/v1alpha1`. That is fine for iteration,
but production users need a compatibility contract.

Evidence in repo:

- CRDs are generated for `v1alpha1`.
- There is no conversion webhook strategy or versioned API compatibility policy.
- There are no compatibility fixtures showing old objects continuing to work.

Required before GA:

- Define the API support policy before the first production release.
- Decide whether the first production API is still `v1alpha1` or moves to
  `v1beta1`.
- Add conversion and migration tests before introducing breaking schema changes.
- Document deprecation windows for fields, plans, and product contracts.

### Supply-chain security needs release-grade controls

The GitHub workflows build and publish images, but production distribution needs
stronger provenance and scanning.

Evidence in repo:

- Build workflow publishes images to GHCR.
- Dependabot and gitleaks are configured.
- There is no visible `govulncheck`, `gosec`, container vulnerability scan, SBOM
  generation, image signing, provenance attestation, or SLSA-style release
  metadata.
- Containerfiles download external binaries such as Helm and kubectl without
  checksum verification.
- Images are built for amd64 only.

Required before GA:

- Add Go vulnerability scanning and static security checks.
- Add image vulnerability scanning for every image.
- Generate SBOMs for release artifacts.
- Sign images and release manifests with cosign.
- Publish provenance attestations.
- Verify checksums for downloaded tool binaries.
- Add multi-arch image builds if Linux arm64 clusters are supported.

### Web/BFF edge hardening is incomplete

The web and BFF layers need standard production edge controls.

Evidence in repo:

- The nginx web image proxies to BFF but does not appear to define a full set of
  security headers.
- Production manifests do not include ingress TLS, HSTS, CSP, frame protection,
  referrer policy, or request size/rate controls.
- BFF cookies are secure only when the request appears to be HTTPS or
  `X-Forwarded-Proto=https`.

Required before GA:

- Add a documented reverse-proxy contract for TLS termination.
- Add security headers for the web app.
- Add request size and rate limits at ingress or BFF.
- Add explicit trusted proxy handling for forwarded headers.
- Add smoke tests for login/session behavior behind ingress.

## P2 Scale and Maturity Gaps

### Multi-tenancy boundaries need threat-model validation

Servicer has tenant/project concepts and role bindings, but production needs a
clear isolation model.

Needed:

- Threat model tenant admin, project admin, catalog admin, platform admin, and
  auditor roles.
- Prove tenant-scoped users cannot read or mutate cross-tenant secrets,
  repositories, applications, credentials, namespaces, or audit records.
- Add negative authorization tests for every BFF endpoint.
- Document which Kubernetes namespaces and Argo CD projects are considered
  tenant boundaries.

### Repository management needs tenant-scoped lifecycle guarantees

Repositories are now part of the admin experience, but production users need
clear ownership and credential isolation.

Needed:

- Ensure repositories can be scoped per tenant and project where intended.
- Store repository credentials with least privilege and clear rotation behavior.
- Audit repository create, update, delete, and credential access.
- Validate Argo CD repository registration for private repos in e2e tests.
- Define what happens when a repository is deleted while active services depend
  on it.

### Service deletion and data-retention semantics need hardening

Production users need predictable behavior when deleting services that may own
data.

Needed:

- Make deletion policies explicit per plan and product.
- Add guardrails for destructive deletes, especially database, volume, and VM
  products.
- Validate finalizers, orphan handling, and failed deletion recovery.
- Add restore guidance for accidental deletion.

### Scale limits and performance envelopes are undefined

There are no documented limits for tenants, projects, services, repositories,
ActionRequests, audit records, or watched resources.

Needed:

- Define target scale for the first production release.
- Load test BFF list/detail endpoints and controller reconcile loops.
- Add pagination or server-side filtering where list endpoints can grow large.
- Watch Kubernetes API QPS, cache memory, and Argo CD API behavior under load.

## Product-Specific Gaps

- Namespace: usable foundation. Quota, network policy, deletion, and tenant
  isolation need e2e coverage.
- PostgreSQL/CNPG: stronger than most products. Backup, restore, failover,
  credential rotation, and operator upgrade drills need cluster tests.
- MySQL: manifest support exists. Backup/restore actions and topology failover
  need production execution paths and tests.
- Valkey: manifest/action paths exist. Failover and health behavior need real
  operator/runtime validation.
- NATS: multi-cluster rendering exists. JetStream, gateway health, replication
  lag, and recovery drills need real validation.
- YugabyteDB: xCluster rendering exists. xCluster setup, backup, restore,
  failover, and lag telemetry need operator-deep validation.
- Cassandra/K8ssandra: partial. Backup, repair, health, and action execution are
  not production-ready.
- KubeVirt/VM: partial. Guest readiness, storage/DataVolume status, power
  actions, console/access patterns, and backup policy need work.
- Argo-backed applications: useful path exists. Private repo credentials,
  per-tenant scoping, failure recovery, and delivery drift need e2e validation.

## Suggested Milestones

### P0: Production-safe install

- Harden `deploy/` or add a production overlay/Helm chart.
- Require production session secrets.
- Configure GitOps delivery by default.
- Complete backup/restore coverage.
- Split RBAC by component.
- Publish versioned, signed release artifacts.

### P1: GA confidence

- Add e2e install/provision/backup/restore tests.
- Package observability.
- Complete audit coverage.
- Add supply-chain scanning, SBOMs, signatures, and provenance.
- Publish product/action support matrix.
- Define API compatibility policy.

### P2: Maturity

- Deepen product adapters.
- Load test and document scale limits.
- Complete tenant threat-model validation.
- Add deletion/data-retention guardrails.
- Add upgrade and rollback playbooks.

## Already In Place

These are not production gaps, but they are important foundations to preserve:

- CRD-backed domain APIs for tenants, projects, clusters, catalog, service
  instances, actions, auth resources, policies, namespace claims, VM claims, and
  service bindings.
- BFF and web app flows for core platform administration.
- Metrics do not have authentication
- GHCR image publishing from GitHub Actions.
- Release manifest rendering for tagged builds.
- Secret scanning and dependency update automation.
- Unit tests and kustomize validation in CI.
- A distinct `deploy/` directory for production assets, separate from demo/local
  validation assets.
