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

## P2 Scale and Maturity Gaps

No open P2 gaps remain in this tracker. Production contracts now live in:

- `docs/tenant-threat-model.md`
- `docs/repository-lifecycle.md`
- `docs/deletion-retention.md`
- `docs/scale-limits.md`
- `docs/product-runbooks.md`

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
