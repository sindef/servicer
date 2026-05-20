# 003: Delivery Engine And Cluster Adapters

## Summary

Turn Servicer CRDs into deterministic delivery artifacts and reconcile them into target clusters through Argo CD.

## Why This Exists

The architecture only works if the platform can convert product intent into predictable cluster materialization without exposing Git as the user workflow.

## Scope

- implement the render and materialize pipeline
- define the generated delivery repository layout
- register and validate ClusterTarget resources
- create the Argo CD Application and ApplicationSet strategy
- integrate Vault and secret projection for runtime credentials
- normalize sync and health status back into Servicer CRDs

## Acceptance Criteria

- one target cluster can be onboarded and validated
- one NamespaceClaim can be rendered, synced, and observed through Argo CD
- one CNPG-backed PostgreSQL instance can be provisioned end to end
- ServiceInstance status reflects render, sync, and runtime phases clearly
- generated artifacts are deterministic and auditable

## Out Of Scope

- full product catalog breadth
- a polished user interface

## Notes

Generated Git should stay controller-owned. Do not allow human edits to become part of the normal reconcile path.