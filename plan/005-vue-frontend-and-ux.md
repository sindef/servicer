# 005: Vue Frontend And UX

## Summary

Build the first production-shaped Vue frontend and BFF experience for tenants, projects, product requests, and day-2 operations.

## Why This Exists

The whole point of Servicer is to replace a generic portal with a purpose-built product experience. This phase turns the API and runtime work into something users can actually operate.

## Scope

- create the Vue application shell and design system primitives
- implement authentication and tenant-aware navigation
- build the catalog request flow
- build unified instance list and detail pages
- build action request flows and operation timelines
- expose audit trails, sync state, and failure localization through the BFF

## Acceptance Criteria

- a user can request a supported product without kubectl or direct Git interaction
- an operator can identify whether a failure occurred in validation, render, Argo sync, or runtime reconcile
- instance detail pages surface health, topology, endpoints, and recent actions clearly
- the interface remains usable for both service consumers and platform operators

## Out Of Scope

- every advanced operator-specific feature
- full reporting, billing, or chargeback

## Notes

Do not ship a thin CRUD UI over raw CRDs. The BFF should return product-shaped responses and the frontend should be workflow-first.