# Catalog Lifecycle

Production catalog management is GitOps-managed for the first production release. Operators should review catalog changes in Git, apply them through the Servicer production delivery repository, and let the manager reconcile required operator packages onto cluster targets.

## Production Support Set

Production-supported service classes:

- `namespace`: no external operator package required
- `postgresql`: requires `cnpg`
- `yugabyte`: requires `yugabyte`
- `argo-application`: requires Argo CD already installed in the platform cluster

Preview service classes:

- `mysql`
- `valkey`
- `nats`
- `k8ssandra`
- `virtual-machine`

Preview classes can remain in samples and development overlays, but production paths must not advertise them until their operator packages, health semantics, action behavior, and restore drills are complete.

## Version Policy

Operator packages in `deploy/operator-packages.yaml` are pinned to explicit versions, release branches, or reviewed commits. Production upgrades happen through pull requests that change:

- `OperatorPackage.spec.version`
- source URL, chart archive, path, or target revision
- service class supported versions
- plan compatibility notes

Rollback is the inverse Git change plus operator-specific rollback steps from upstream documentation. Rollbacks that may affect data-bearing services require an approved `ActionRequest` or a platform change ticket.

## Compatibility Guidance

`namespace` plans are compatible across Servicer patch releases unless a policy field is explicitly deprecated.

`postgresql` plans track CloudNativePG major/minor compatibility. PostgreSQL engine version upgrades must be tested on a restored copy before production rollout.

`yugabyte` plans track YugabyteDB operator and database compatibility together. xCluster, failover, and restore behavior must be tested before changing the operator package.

`argo-application` plans require repository credentials and Argo CD project policy to be reviewed for each tenant/project.

## Sample Defaults

Sample defaults live under `config/samples`. Production install paths use only curated operator package seeds and must not copy sample credentials, local kubeconfigs, or local-only plan values into customer clusters without review.
