#!/usr/bin/env bash
# hack/demo-setup.sh — one-time setup for the Servicer K8s demo.
#
# Creates two Kind clusters:
#   servicer-app    — runs the Servicer platform (manager, bff, web, syncer)
#   servicer-target — the managed cluster where delivery artifacts land
#
# Builds all four container images, loads them into servicer-app, installs
# CRDs, applies the service catalog and demo tenancy, and patches the
# local-dev-kubeconfig Secret so the syncer can reach servicer-target from
# inside a pod.
#
# Run once, then open http://localhost:5173
# Re-run to rebuild images and redeploy without touching the clusters.
set -euo pipefail

APP_CLUSTER="${APP_CLUSTER:-servicer-app}"
TARGET_CLUSTER="${TARGET_CLUSTER:-servicer-target}"

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: required command not found: $1" >&2
    exit 1
  fi
}

require kind
require kubectl
require docker

mkdir -p generated

# ── App cluster ───────────────────────────────────────────────────────────────
if kind get clusters 2>/dev/null | grep -qx "${APP_CLUSTER}"; then
  echo "Kind cluster '${APP_CLUSTER}' already exists."
else
  echo "Creating Kind cluster '${APP_CLUSTER}'..."
  kind create cluster \
    --name "${APP_CLUSTER}" \
    --config config/kind/app.yaml \
    --wait 120s
fi

# ── Target cluster ────────────────────────────────────────────────────────────
if kind get clusters 2>/dev/null | grep -qx "${TARGET_CLUSTER}"; then
  echo "Kind cluster '${TARGET_CLUSTER}' already exists."
else
  echo "Creating Kind cluster '${TARGET_CLUSTER}'..."
  kind create cluster \
    --name "${TARGET_CLUSTER}" \
    --config config/kind/target.yaml \
    --wait 120s
fi

# ── Build images ──────────────────────────────────────────────────────────────
echo "Building images..."
docker build -t servicer/manager:demo -f Containerfile.manager .
docker build -t servicer/bff:demo     -f Containerfile.bff .
docker build -t servicer/web:demo     -f Containerfile.web .
docker build -t servicer/syncer:demo  -f Containerfile.syncer .

# ── Load images into app cluster ──────────────────────────────────────────────
echo "Loading images into '${APP_CLUSTER}'..."
kind load docker-image servicer/manager:demo --name "${APP_CLUSTER}"
kind load docker-image servicer/bff:demo     --name "${APP_CLUSTER}"
kind load docker-image servicer/web:demo     --name "${APP_CLUSTER}"
kind load docker-image servicer/syncer:demo  --name "${APP_CLUSTER}"

# ── Switch to app cluster ─────────────────────────────────────────────────────
kubectl config use-context "kind-${APP_CLUSTER}" >/dev/null

# ── CRDs ─────────────────────────────────────────────────────────────────────
echo "Applying CRDs..."
kubectl apply -f config/crd/bases/
kubectl wait --for=condition=Established crd --all --timeout=120s

# ── App manifests ─────────────────────────────────────────────────────────────
echo "Applying deploy manifests..."
kubectl apply -k config/deploy/

# ── Service catalog + tenancy samples ─────────────────────────────────────────
echo "Applying samples (catalog, tenancy)..."
kubectl apply -k config/samples/

# ── Target kubeconfig (pod-reachable) ─────────────────────────────────────────
# The syncer pod runs inside the app cluster and needs to reach the target
# cluster's API server. Kind nodes share the 'kind' Docker bridge network, so
# the target control-plane container IP is routable from pods inside the app
# cluster. The node IP is always included in Kind's generated TLS cert SANs.
echo "Patching local-dev-kubeconfig with target cluster address..."

TARGET_NODE="${TARGET_CLUSTER}-control-plane"
TARGET_IP=$(docker inspect "${TARGET_NODE}" \
  --format '{{.NetworkSettings.Networks.kind.IPAddress}}' 2>/dev/null || \
  docker inspect "${TARGET_NODE}" \
  --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')

if [[ -z "${TARGET_IP}" ]]; then
  echo "error: could not determine IP of ${TARGET_NODE}" >&2
  exit 1
fi

# kind get kubeconfig writes server: https://127.0.0.1:6444 (host port).
# Replace with the internal container IP:6443 (internal Kind API port).
TARGET_KUBECONFIG=$(kind get kubeconfig --name "${TARGET_CLUSTER}" | \
  sed "s|https://127.0.0.1:6444|https://${TARGET_IP}:6443|g")

kubectl create secret generic local-dev-kubeconfig \
  --namespace servicer-system \
  --from-literal=kubeconfig="${TARGET_KUBECONFIG}" \
  --dry-run=client -o yaml | kubectl apply -f -

echo ""
echo "Setup complete."
echo "  App cluster    : kind-${APP_CLUSTER}  (API :6443)"
echo "  Target cluster : kind-${TARGET_CLUSTER} (API :6444, pod-reachable at ${TARGET_IP}:6443)"
echo ""
echo "Open http://localhost:5173"
echo ""
echo "To rebuild and redeploy after code changes:"
echo "  ./hack/demo-setup.sh"
echo ""
echo "Teardown:"
echo "  kind delete cluster --name ${APP_CLUSTER}"
echo "  kind delete cluster --name ${TARGET_CLUSTER}"
