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
| `NamespaceClaim` | First-class managed namespace request |
| `ServiceBinding` | Credential sharing between services and consumers |
| `VirtualMachineClaim` | Curated virtual machine request surface |
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

### Docker demo

Runs the app against a local Kind cluster. Requires Docker Engine and `kind` + `kubectl` on your host. Port 8080 must be free.

**Step 1 — create the cluster (once):**

```bash
./hack/demo-setup.sh
```

Creates a Kind cluster named `servicer-demo`, installs CRDs, and applies the demo catalog and tenancy. Writes a standalone kubeconfig to `generated/demo-kubeconfig`. Re-run any time you change CRDs or `config/samples/`.

**Step 2 — start the app:**

```bash
docker compose -f docker-compose.demo.yml up --build
```

Open **http://localhost:8080**.

Services started:
- `manager` — reconciles `ServiceInstance` CRDs, materialises delivery artifacts
- `syncer` — applies generated manifests to the Kind cluster every 3 s (`bitnami/kubectl`)
- `bff` — the Servicer API backend on `:8090`
- `web` — nginx serving the Vue SPA, proxies `/api/` → bff

All services use `network_mode: host` so they reach the Kind API server at `127.0.0.1:6443` exactly as in local development — no kubeconfig patching or extra networking.

**Rebuild after code changes (cluster is unaffected):**

```bash
docker compose -f docker-compose.demo.yml up --build --no-deps bff
docker compose -f docker-compose.demo.yml up --build --no-deps manager
docker compose -f docker-compose.demo.yml up --build --no-deps web
```

**Tear down:**

```bash
docker compose -f docker-compose.demo.yml down
kind delete cluster --name servicer-demo
```

---

### Local development (go run)

**Prerequisites:** Go 1.23+, Node 20+, `kind`, `kubectl`

```bash
./hack/kind-demo.sh
```

Creates a Kind cluster, installs CRDs, and starts the BFF + controller + demo sync loop directly via `go run`. Use this for active development where you want fast recompile cycles.

### Build

```bash
go build ./cmd/bff
go build ./cmd/manager
cd web && npm ci && npm run build
```

### Run BFF

```bash
./bff --listen :8090 --tls-listen :8443
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

### Control-plane backup

Best-effort backup and restore tooling for Servicer control-plane state:

```bash
./hack/control-plane-backup.sh backup
./hack/control-plane-backup.sh restore ./backups/servicer-YYYYMMDD-HHMMSS
```

This snapshots:
- Servicer cluster-scoped CRDs
- `servicer-system` Secrets, ConfigMaps, ServiceAccounts, Roles, RoleBindings
- Servicer-managed Argo CD `Application` objects
- local `generated/` delivery tree when present

This is not full etcd backup, but useful for demo recovery, migration drills, and control-plane state export/import.

### CRD generation

Requires `controller-gen v0.17.3`:

```bash
controller-gen crd paths="./api/..." output:crd:artifacts:config=config/crd/bases
controller-gen object paths="./api/..."
```

## Delivery

Services are delivered via Argo CD. Servicer CRDs are the authoritative source of desired state — not Git-first workflows. The controller materialises Argo `Application` objects from `ServiceInstance` specs.

For Argo-backed delivery beyond the local demo flow, the manager can be configured with:

- `--delivery-repo-url` for the Git repo Argo should track
- `--delivery-repo-path` for the repo-relative root that receives generated packages
- `--delivery-repo-worktree` for a local checked-out worktree that Servicer publishes into
- `--delivery-repo-auto-commit` to create local Git commits after publication
- `--argocd-namespace` and `--argocd-project` for managed `Application` placement

## Product Standards

- [Product standards](docs/product-standards.md) - shared product contract rules, including relational database naming defaults and normalization

## License

Copyright 2026 Michael Norris. Licensed under the [Apache License, Version 2.0](LICENSE).
