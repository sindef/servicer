#!/usr/bin/env bash
# hack/demo-setup.sh — one-time setup for the docker-compose demo.
#
# Creates a Kind cluster, installs CRDs, and applies the sample catalog and
# tenancy. Writes a standalone kubeconfig to generated/demo-kubeconfig so
# docker-compose services can use it without touching ~/.kube/config.
#
# Run once before: docker compose -f docker-compose.demo.yml up --build
# Re-run any time you change CRDs or config/samples/.
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-servicer-demo}"
KIND_IMAGE="${KIND_IMAGE:-kindest/node:v1.32.2}"
KUBECONFIG_OUT="${KUBECONFIG_OUT:-generated/demo-kubeconfig}"
DELIVERY_ROOT="${DELIVERY_ROOT:-generated/demo-delivery}"

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: required command not found: $1" >&2
    exit 1
  fi
}

require kind
require kubectl

mkdir -p generated

# ── Kind cluster ──────────────────────────────────────────────────────────────
if kind get clusters 2>/dev/null | grep -qx "${CLUSTER_NAME}"; then
  echo "Kind cluster '${CLUSTER_NAME}' already exists."
else
  echo "Creating Kind cluster '${CLUSTER_NAME}'..."
  kind create cluster \
    --name "${CLUSTER_NAME}" \
    --image "${KIND_IMAGE}" \
    --config config/kind/demo.yaml \
    --wait 120s
fi

kubectl config use-context "kind-${CLUSTER_NAME}" >/dev/null

kubectl label nodes --all \
  failure-domain.beta.kubernetes.io/region=local \
  failure-domain.beta.kubernetes.io/zone=local-a \
  topology.kubernetes.io/region=local \
  topology.kubernetes.io/zone=local-a \
  --overwrite 2>/dev/null || true

# ── Kubeconfig ────────────────────────────────────────────────────────────────
# Write a standalone kubeconfig (no other contexts, no credentials merged from
# ~/.kube/config). docker-compose services bind-mount this file.
kind get kubeconfig --name "${CLUSTER_NAME}" > "${KUBECONFIG_OUT}"
echo "Kubeconfig written to ${KUBECONFIG_OUT}."

export KUBECONFIG="${KUBECONFIG_OUT}"

# ── CRDs ─────────────────────────────────────────────────────────────────────
echo "Applying CRDs..."
kubectl apply -f config/crd/bases/
kubectl wait --for=condition=Established crd --all --timeout=120s

# ── Samples ───────────────────────────────────────────────────────────────────
echo "Applying samples (catalog, tenancy)..."
kubectl apply -k config/samples/

# ── Delivery root ─────────────────────────────────────────────────────────────
mkdir -p "${DELIVERY_ROOT}"

echo ""
echo "Setup complete."
echo "  Cluster : kind-${CLUSTER_NAME}"
echo "  Kubeconfig : ${KUBECONFIG_OUT}"
echo ""
echo "Start the demo:"
echo "  docker compose -f docker-compose.demo.yml up --build"
