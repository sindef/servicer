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

## P1 GA Gaps

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
