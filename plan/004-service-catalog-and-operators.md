# 004: Service Catalog And Operators

## Summary

Implement the initial product adapters and establish a credible day-2 operations model for the first service set.

## Why This Exists

A PaaS is only real once product requests map cleanly to supported runtimes and operators, with visible health and safe actions.

## Scope

- implement adapters for Namespace, PostgreSQL, Valkey or cache, and NATS
- define ServiceClass and ServicePlan content for the initial products
- normalize status from the underlying runtimes
- implement ActionRequest handling for a small set of safe operations
- add smoke tests for each supported product adapter
- document operator constraints and supported topologies

## Operator Direction

- PostgreSQL: CloudNativePG
- Cassandra: K8ssandra when the Cassandra wave begins
- Valkey or cache: assume a Servicer-owned control path is likely required; keep the runtime adapter compatible with a beta Valkey operator while avoiding product coupling to it

For Valkey specifically, do not let a beta operator define the platform API. The controller contract should preserve a path to self-managed reconciliation and, later, explicit multi-cluster cache topologies.

## Acceptance Criteria

- the first four products can be provisioned end to end through Servicer APIs
- at least one safe day-2 action works for each product family where applicable
- product status is understandable without reading raw operator CRs
- smoke tests catch major reconciliation or health regressions

## Stretch Scope

- begin KubeVirt adapter work if the first four products stabilize quickly

## Notes

This is the phase where the product either becomes believable or remains a diagram. Keep the adapter surfaces small and explicit.