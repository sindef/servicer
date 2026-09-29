#!/usr/bin/env bash
set -euo pipefail

CLUSTER="${CLUSTER:-servicer-upgrade-e2e}"
KIND_IMAGE="${KIND_IMAGE:-kindest/node:v1.32.2}"
PREVIOUS_VERSION="${PREVIOUS_VERSION:-v0.1.0}"
CURRENT_VERSION="${CURRENT_VERSION:-$(git describe --tags --always --dirty)}"

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require kind
require kubectl

if ! kind get clusters | grep -qx "${CLUSTER}"; then
  kind create cluster --name "${CLUSTER}" --image "${KIND_IMAGE}"
fi
kubectl config use-context "kind-${CLUSTER}" >/dev/null
kubectl apply -f config/crd/bases
# The deploy render now carries the Servicer Prometheus rules and ServiceMonitors
# from config/observability, so the monitoring CRDs are part of the install
# contract and must exist before the server-side dry-run below (same upstream
# manifest fetch pattern as the product operator e2e).
kubectl apply -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v0.80.0/example/prometheus-operator-crd/monitoring.coreos.com_prometheusrules.yaml
kubectl apply -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v0.80.0/example/prometheus-operator-crd/monitoring.coreos.com_servicemonitors.yaml
kubectl wait --for=condition=Established crd --all --timeout=90s
kubectl apply -f api/v1alpha1/fixtures/stored-objects.yaml

./hack/render-deploy-manifest.sh "${PREVIOUS_VERSION}" >/tmp/servicer-previous.yaml
./hack/render-deploy-manifest.sh "${CURRENT_VERSION}" >/tmp/servicer-current.yaml
kubectl apply --dry-run=server -f /tmp/servicer-previous.yaml >/dev/null
kubectl apply --dry-run=server -f /tmp/servicer-current.yaml >/dev/null
kubectl apply --dry-run=server -f api/v1alpha1/fixtures/stored-objects.yaml >/dev/null

echo "Upgrade e2e dry-run passed from ${PREVIOUS_VERSION} to ${CURRENT_VERSION}."
