# 002: Domain APIs And Persistence

## Summary

Implement the Servicer CRD model and make the management cluster the authoritative persistence layer for platform state.

## Why This Exists

The control plane cannot depend on ad hoc portal state or Git commits for core product behavior. It needs a typed API, validation, status conventions, and tenancy boundaries.

## Scope

- define CRDs for Tenant, Project, ClusterTarget, ServiceClass, ServicePlan, ServiceInstance, NamespaceClaim, VirtualMachineClaim, ServiceBinding, and ActionRequest
- implement admission validation and defaulting
- define condition and event conventions
- implement tenancy, quota, and placement policy baselines
- generate typed clients and shared API packages
- establish audit attribution for writes

## First Concrete Deliverables

The first implementation wave should begin with the detailed contracts in:

- [docs/crd-specs-v1alpha1.md](../docs/crd-specs-v1alpha1.md)
- [docs/controller-contracts.md](../docs/controller-contracts.md)

The initial coded surface should prioritize these resources before widening the API:

- Tenant
- Project
- ServiceInstance
- ActionRequest

This gives the platform a usable end-to-end path for ownership, placement, provisioning, and audited day-2 actions before expanding into the rest of the CRD set.

## Acceptance Criteria

- the CRDs install cleanly and support versioned evolution
- a tenant and project can be created with policy validation
- a service request can be persisted as a ServiceInstance
- status fields communicate validation, placement, and reconcile progress
- audit metadata is attached to all write paths

## Out Of Scope

- deep operator integration beyond basic status placeholders
- rich frontend workflows
- full multi-cluster delivery

## Notes

Do not let this phase collapse into one giant generic CRD. Keep the model opinionated and typed.