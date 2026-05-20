# Servicer

Kubernetes-native control plane for self-service managed services. Purpose-built alternative to developer portals — delivers a PaaS-like UX backed by authoritative Kubernetes CRDs.

## Overview

Servicer lets platform teams publish curated service products (PostgreSQL, MySQL, Valkey, NATS, namespaces) that users provision via a product-shaped UI and API. No raw YAML, no operator forms. Operations are task-oriented: request, observe, act.

```
User → Vue UI → BFF (:8090) → Kubernetes CRDs → Operators/Argo CD
```

## Architecture

| Layer | Path | Purpose |
|-------|------|---------|
| CRD/API | `api/v1alpha1/` | Cluster-scoped Kubernetes resources |
| BFF | `cmd/bff/`, `internal/bff/` | HTTP aggregation server — product-shaped responses |
| Controller | `cmd/manager/`, `internal/controllers/` | CRD reconciliation |
| Adapters | `internal/adapters/` | Normalized driver contracts (PostgreSQL, MySQL, Valkey, NATS) |
| Frontend | `web/` | Vue 3 + TypeScript UI |

### CRDs

| Resource | Purpose |
|----------|---------|
| `Tenant` | Tenancy boundary — owners, allowed service classes, quotas |
| `Project` | Cluster placement, namespace strategy; child of Tenant |
| `ServiceClass` | Product definition — driver, capabilities |
| `ServicePlan` | Tiers, topology, versions |
| `ServiceInstance` | Provisioned service — phase, health, endpoints |
| `ActionRequest` | Day-2 operations (scale, failover, etc.) |
| `ClusterTarget` | Target cluster definitions |

API group: `platform.servicer.io/v1alpha1`

### BFF Endpoints

```
GET  /api/overview                  Health dashboard rollup
GET  /api/tenants                   Tenant list
GET  /api/projects                  Project list
GET  /api/catalog                   Available service classes + plans
POST /api/requests                  Provision a new service
GET  /api/instances                 All provisioned instances
GET  /api/instances/{name}          Full instance detail (topology, conditions, endpoints)
PUT  /api/instances/{name}          Update instance
DELETE /api/instances/{name}        Delete instance
POST /api/instances/{name}/actions  Submit day-2 ActionRequest
GET  /api/audit                     Queryable audit trail
```

### Frontend Routes

| Route | View |
|-------|------|
| `/` | Overview — health dashboard |
| `/catalog` | Request a new service |
| `/instances` | Provisioned services (filterable) |
| `/instances/:name` | Instance detail — topology, actions, endpoints |
| `/tenancy` | Tenants + projects |
| `/audit` | Change audit trail |

## Supported Services

| Service | Status |
|---------|--------|
| Kubernetes Namespace | Available |
| PostgreSQL (CloudNativePG) | Available |
| MySQL (Servicer-owned) | Available |
| Valkey / Redis | Available |
| NATS (JetStream) | Available |
| YugabyteDB | Planned |
| Cassandra (K8ssandra) | Planned |
| KubeVirt | Planned |

## Getting Started

### Prerequisites

- Go 1.23+
- Node 20+
- Kubernetes cluster (or [kind](https://kind.sigs.k8s.io/))
- `kubectl` configured

### Run locally (kind demo)

```bash
./hack/kind-demo.sh
```

This creates a local kind cluster, installs CRDs, and starts the BFF + demo sync.

### Build

```bash
# Go binaries
go build ./cmd/bff
go build ./cmd/manager

# Frontend
cd web && npm ci && npm run build
```

### Run BFF

```bash
./bff --kubeconfig ~/.kube/config
# Serves on :8090
```

### Run frontend (dev)

```bash
cd web && npm run dev
# Serves on :5173, proxies /api → :8090
```

### Install CRDs

```bash
kubectl apply -f config/crd/bases/
```

### Sample resources

```bash
kubectl apply -f config/samples/
```

## Development

### Tests

```bash
go test ./...
```

### CRD generation

Requires `controller-gen v0.17.3`:

```bash
controller-gen crd paths="./api/..." output:crd:artifacts:config=config/crd/bases
controller-gen object paths="./api/..."
```

## Delivery

Services are delivered via Argo CD. Servicer CRDs are the authoritative source of desired state — not Git-first workflows. The controller materialises Argo `Application` objects from `ServiceInstance` specs.

## Product Standards

- [Product standards](docs/product-standards.md) - shared product contract rules, including relational database naming defaults and normalization

## License

Copyright 2026 Michael Norris. Licensed under the [Apache License, Version 2.0](LICENSE).
