# Feature Gap Analysis

This document tracks product areas that remain partial or preview as of `v0.1.0`.

## Cassandra (K8ssandra)

Current state:

- Provisioning path exists for catalog and instance workflows.
- Product is not enabled in the production support set.

Remaining gaps before production support:

- Automated backup and restore drills.
- Ring repair and failure recovery runbooks validated in CI.
- Clear degraded health semantics in BFF/API responses.

## Virtual Machine (KubeVirt)

Current state:

- VM claim workflows are available for initial provisioning.
- Product is not enabled in the production support set.

Remaining gaps before production support:

- Guest readiness and boot diagnostics surfaced consistently in status.
- Power lifecycle and restart actions hardened with tests.
- Storage resize and attachment behaviors validated with operator drills.

## Preview Product Guardrails

For any preview product moving toward production support:

- Add product drills to automated e2e coverage.
- Update [Product support matrix](product-support-matrix.md).
- Update production catalog and operator package pins in the same release.

## Production Hardening Gaps

Current launch-grade behavior is documented in the production, audit, repository lifecycle, threat model, and scale-limit docs. Known remaining gaps that require operator awareness:

- Login rate limiting only has a per-pod memory backend; production deployments need explicit acceptance and compensating IdP/ingress controls if multiple BFF replicas are used.
- Audit records persist mutating API requests and credential reveal/download requests, but auth login/logout evidence still depends on IdP logs and BFF JSON logs.
- High-volume BFF list endpoints support `limit`, `offset`, and `q`, but responses do not yet include total counts or continuation tokens.

## Launch Exceptions (Owned)

The current deferred exceptions and blockers are tracked in [plan/production-readiness-checklist.md](/home/mnorris/repos/servicer/plan/production-readiness-checklist.md). Exceptions that require explicit owner sign-off before GA:

- Finding 8 (per-replica login limiter): Owner `Platform security`.
- Finding 10 (namespace token lookup model): Owner `BFF/API`.
- Finding 30 (residual manager RBAC breadth): Owner `Platform controllers`.
- Finding 33 (network egress overlays): Owner `SRE/networking`.
- Finding 42 (credential reveal re-auth/MFA backend enforcement): Owner `Security UX + auth`.
- Finding 46 (committed `web/dist` policy): Owner `Release engineering`.

Active launch blockers are tracked in [plan/do-not-ship.md](/home/mnorris/repos/servicer/plan/do-not-ship.md).
