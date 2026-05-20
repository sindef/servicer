#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-paas-demo-008}"
BFF_ADDR="${BFF_ADDR:-:8090}"
BFF_TLS_ADDR="${BFF_TLS_ADDR:-:8443}"
WEB_PORT="${WEB_PORT:-5173}"
DELIVERY_ROOT="${DELIVERY_ROOT:-$(pwd)/generated/demo-delivery}"

cleanup() {
  for pid in "${PIDS[@]:-}"; do
    kill "${pid}" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT

PIDS=()

kubectl config use-context "kind-${CLUSTER_NAME}" >/dev/null
kubectl apply -f config/crd/bases >/dev/null
kubectl wait --for=condition=Established crd --all --timeout=90s
hack/demo-yugabyte-operator.sh
kubectl apply -k config/samples >/dev/null
mkdir -p "${DELIVERY_ROOT}"

go run ./cmd/manager --metrics-bind-address=:0 --health-probe-bind-address=:18081 --delivery-root "${DELIVERY_ROOT}" &
PIDS+=("$!")

go run ./cmd/bff --listen "${BFF_ADDR}" --tls-listen "${BFF_TLS_ADDR}" &
PIDS+=("$!")

hack/demo-sync-delivery.sh &
PIDS+=("$!")

npm run dev --prefix web -- --port "${WEB_PORT}" --host 0.0.0.0 &
PIDS+=("$!")

echo "Servicer demo is starting:"
echo "  UI:  http://localhost:${WEB_PORT}"
echo "  BFF: http://localhost:${BFF_ADDR#:}"
echo "  Kubernetes access proxy: https://localhost:${BFF_TLS_ADDR#:}"
echo "Press Ctrl-C to stop manager, BFF, delivery sync, and Vite."

wait
