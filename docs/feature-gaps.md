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
