#!/usr/bin/env bash
set -euo pipefail

CLUSTER="${CLUSTER:-servicer-products-e2e}"
KIND_IMAGE="${KIND_IMAGE:-kindest/node:v1.32.2}"

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require kind
require kubectl
require go

if ! kind get clusters | grep -qx "${CLUSTER}"; then
  kind create cluster --name "${CLUSTER}" --image "${KIND_IMAGE}"
fi
kubectl config use-context "kind-${CLUSTER}" >/dev/null
kubectl apply -f config/crd/bases
kubectl wait --for=condition=Established crd --all --timeout=90s

kubectl apply -f https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.25/releases/cnpg-1.25.4.yaml
kubectl wait -n cnpg-system --for=condition=Available deploy/cnpg-controller-manager --timeout=180s

kubectl apply -k config/samples
go test ./internal/adapters ./internal/controllers

echo "Product operator e2e baseline passed for production-supported operator-backed products."
