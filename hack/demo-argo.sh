#!/usr/bin/env bash
# hack/demo-argo.sh — end-to-end demo of the Argo CD Application product.
#
# What it does:
#   1. Creates a Tenant and Project (if they don't already exist).
#   2. Registers a public Git repository under that Project (no credentials).
#   3. Creates a ServiceInstance of class argo-application that points at the
#      argoproj/argocd-example-apps guestbook chart.
#   4. Waits for the manager to materialise the Application YAML.
#   5. Shows the generated manifest.
#   6. Applies it via kubectl (same path as demo-sync-delivery.sh).
#
# Usage:
#   hack/demo-argo.sh
#   BFF_ADDR=http://localhost:8090 hack/demo-argo.sh
#
# Requirements: curl, kubectl, jq
set -euo pipefail

BFF_ADDR="${BFF_ADDR:-http://localhost:8090}"
DELIVERY_ROOT="${DELIVERY_ROOT:-$(pwd)/generated/demo-delivery}"
CLUSTER_NAME="${CLUSTER_NAME:-paas-demo-008}"

TENANT_NAME="demo-tenant"
PROJECT_NAME="demo-project"
REPO_NAME="argocd-examples"
INSTANCE_NAME="guestbook"

# In demo mode the BFF accepts role headers directly.
AUTH_HEADERS=(
  -H "X-Servicer-Actor: demo-admin"
  -H "X-Servicer-Roles: platform-admin,tenant-operator,service-consumer"
)

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

bff() {
  local method="$1"
  local path="$2"
  shift 2
  curl -fsSL -X "${method}" "${BFF_ADDR}${path}" \
    "${AUTH_HEADERS[@]}" \
    -H "Content-Type: application/json" \
    "$@"
}

require curl
require kubectl
require jq

echo "==> Checking BFF is reachable at ${BFF_ADDR}..."
bff GET /api/healthz >/dev/null

# ── 1. Tenant ──────────────────────────────────────────────────────────────────
echo "==> Ensuring tenant '${TENANT_NAME}'..."
if ! bff GET /api/tenants 2>/dev/null | jq -e ".[] | select(.name == \"${TENANT_NAME}\")" >/dev/null 2>&1; then
  bff POST /api/admin/tenants -d @- <<EOF | jq -r '.message'
{
  "name": "${TENANT_NAME}",
  "displayName": "Demo Tenant",
  "owners": ["demo-admin"],
  "allowedServiceClasses": ["namespace", "argo-application"]
}
EOF
else
  echo "   already exists"
fi

# ── 2. Project ─────────────────────────────────────────────────────────────────
echo "==> Ensuring project '${PROJECT_NAME}'..."
if ! bff GET /api/projects 2>/dev/null | jq -e ".[] | select(.name == \"${PROJECT_NAME}\")" >/dev/null 2>&1; then
  bff POST /api/admin/projects -d @- <<EOF | jq -r '.message'
{
  "name": "${PROJECT_NAME}",
  "displayName": "Demo Project",
  "tenantName": "${TENANT_NAME}",
  "environment": "demo",
  "clusterName": "${CLUSTER_NAME}"
}
EOF
else
  echo "   already exists"
fi

# ── 3. Repository ──────────────────────────────────────────────────────────────
echo "==> Registering repository '${REPO_NAME}' under project '${PROJECT_NAME}'..."
if ! bff GET "/api/projects/${PROJECT_NAME}/repositories" 2>/dev/null | jq -e ".[] | select(.name == \"${REPO_NAME}\")" >/dev/null 2>&1; then
  bff POST "/api/projects/${PROJECT_NAME}/repositories" -d @- <<EOF | jq -r '.message'
{
  "name": "${REPO_NAME}",
  "displayName": "Argo CD Example Apps",
  "projectName": "${PROJECT_NAME}",
  "url": "https://github.com/argoproj/argocd-example-apps.git",
  "authType": "none"
}
EOF
else
  echo "   already exists"
fi

REPO_URL="$(bff GET "/api/projects/${PROJECT_NAME}/repositories" | jq -r ".[] | select(.name == \"${REPO_NAME}\") | .url")"

# ── 4. ServiceInstance ─────────────────────────────────────────────────────────
echo "==> Ensuring ServiceInstance '${INSTANCE_NAME}'..."
if ! bff GET /api/instances 2>/dev/null | jq -e ".[] | select(.name == \"${INSTANCE_NAME}\")" >/dev/null 2>&1; then
  bff POST /api/requests -d @- <<EOF | jq -r '.message'
{
  "name": "${INSTANCE_NAME}",
  "projectName": "${PROJECT_NAME}",
  "serviceClass": "argo-application",
  "servicePlan": "argo-application-standard",
  "parameters": {
    "repoURL": "${REPO_URL}",
    "repoRef": "${REPO_NAME}",
    "path": "guestbook",
    "targetRevision": "HEAD",
    "targetNamespace": "${PROJECT_NAME}-guestbook",
    "syncPolicy": "auto",
    "createNamespace": true
  }
}
EOF
else
  echo "   already exists"
fi

# ── 5. Wait for materialisation ────────────────────────────────────────────────
MANIFEST_PATH="${DELIVERY_ROOT}/clusters/${CLUSTER_NAME}/argo-apps/${INSTANCE_NAME}/application.yaml"
echo "==> Waiting for manifest at ${MANIFEST_PATH}..."
for i in $(seq 1 20); do
  if [[ -f "${MANIFEST_PATH}" ]]; then
    break
  fi
  sleep 2
  echo -n "."
done
echo ""

if [[ ! -f "${MANIFEST_PATH}" ]]; then
  echo "Manifest not found after 40s. Is the manager running?" >&2
  echo "Start it with: hack/demo-serve.sh or go run ./cmd/manager --delivery-root ${DELIVERY_ROOT}" >&2
  exit 1
fi

# ── 6. Show the manifest ───────────────────────────────────────────────────────
echo ""
echo "==> Generated Argo CD Application manifest:"
echo "--------------------------------------------"
cat "${MANIFEST_PATH}"
echo "--------------------------------------------"

# ── 7. Apply it ────────────────────────────────────────────────────────────────
echo ""
echo "==> Applying to cluster (${CLUSTER_NAME})..."
if ! kubectl get namespace argocd >/dev/null 2>&1; then
  echo "   argocd namespace not found — skipping kubectl apply."
  echo "   Install Argo CD first: kubectl create namespace argocd && kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml"
else
  kubectl apply -f "${MANIFEST_PATH}"
  echo "==> Applied. Check status with:"
  echo "    kubectl get application ${INSTANCE_NAME} -n argocd"
fi

echo ""
echo "Done. To request this product from the UI:"
echo "  1. Open Catalog → Argo CD Application → Standard"
echo "  2. Pick project '${PROJECT_NAME}'"
echo "  3. Choose repository '${REPO_NAME}' from the dropdown"
echo "  4. Set path=guestbook, target-namespace=${PROJECT_NAME}-guestbook, sync=auto"
