#!/usr/bin/env bash
set -euo pipefail

CMD="${1:-up}"
if [[ "${CMD}" != "up" && "${CMD}" != "down" ]]; then
  echo "Usage: $0 [up|down]" >&2
  exit 1
fi

APP_CLUSTER="${APP_CLUSTER:-servicer-app}"
TARGET_CLUSTER="${TARGET_CLUSTER:-servicer-target}"
KIND_IMAGE="${KIND_IMAGE:-kindest/node:v1.32.2}"
BFF_ADDR="${BFF_ADDR:-127.0.0.1:8090}"
BFF_TLS_ADDR="${BFF_TLS_ADDR:-127.0.0.1:8443}"
DELIVERY_ROOT="${DELIVERY_ROOT:-$(pwd)/generated/demo-delivery}"
INSTANCE_NAME="${INSTANCE_NAME:-demo-namespace}"
YUGABYTE_OPERATOR_REF="${YUGABYTE_OPERATOR_REF:-main}"
COOKIE_JAR="$(mktemp)"

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

wait_for_jsonpath() {
  local resource="$1"
  local jsonpath="$2"
  local expected="$3"
  local timeout="${4:-90}"
  local start
  start="$(date +%s)"
  while true; do
    value="$(kubectl get "${resource}" -o "jsonpath=${jsonpath}" 2>/dev/null || true)"
    if [[ "${value}" == "${expected}" ]]; then
      return 0
    fi
    if (( "$(date +%s)" - start > timeout )); then
      echo "timed out waiting for ${resource} ${jsonpath} to become ${expected}; last value: ${value}" >&2
      return 1
    fi
    sleep 2
  done
}

cleanup() {
  rm -f "${COOKIE_JAR}" >/dev/null 2>&1 || true
  if [[ -n "${MANAGER_PID:-}" ]]; then
    kill "${MANAGER_PID}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${BFF_PID:-}" ]]; then
    kill "${BFF_PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

require kind
require kubectl
require go
require npm
require curl

if [[ "${CMD}" == "down" ]]; then
  deleted=0
  for cluster in "${APP_CLUSTER}" "${TARGET_CLUSTER}"; do
    if kind get clusters 2>/dev/null | grep -qx "${cluster}"; then
      kind delete cluster --name "${cluster}"
      echo "Cluster '${cluster}' deleted."
      (( deleted++ )) || true
    else
      echo "Cluster '${cluster}' not found; skipping."
    fi
  done
  [[ ${deleted} -gt 0 ]] || echo "No clusters found; nothing to do."
  exit 0
fi

if ! kind get clusters | grep -qx "${APP_CLUSTER}"; then
  kind create cluster --name "${APP_CLUSTER}" --image "${KIND_IMAGE}"
fi
kubectl config use-context "kind-${APP_CLUSTER}" >/dev/null
kubectl label nodes --all \
  failure-domain.beta.kubernetes.io/region=local \
  failure-domain.beta.kubernetes.io/zone=local-a \
  topology.kubernetes.io/region=local \
  topology.kubernetes.io/zone=local-a \
  --overwrite >/dev/null

kubectl apply -f config/crd/bases
kubectl wait --for=condition=Established crd --all --timeout=90s
YUGABYTE_OPERATOR_REF="${YUGABYTE_OPERATOR_REF}" hack/demo-yugabyte-operator.sh
kubectl apply -k config/samples

go test ./...
npm ci --prefix web
npm run build --prefix web

rm -rf "${DELIVERY_ROOT}"
go run ./cmd/manager --metrics-bind-address=:0 --health-probe-bind-address=:18081 --delivery-root "${DELIVERY_ROOT}" &
MANAGER_PID="$!"
go run ./cmd/bff --listen "${BFF_ADDR}" --tls-listen "${BFF_TLS_ADDR}" &
BFF_PID="$!"

for _ in $(seq 1 60); do
  if curl -fsS "http://${BFF_ADDR}/api/healthz" >/dev/null; then
    break
  fi
  sleep 1
done
curl -fsS "http://${BFF_ADDR}/api/healthz" >/dev/null
curl -fsS -X POST "http://${BFF_ADDR}/api/auth/login" \
  -c "${COOKIE_JAR}" -b "${COOKIE_JAR}" \
  -H "Content-Type: application/json" \
  --data '{"provider":"local","username":"demo-admin","password":"demo-admin"}' >/dev/null

wait_for_jsonpath "tenant/demo" "{.status.phase}" "Ready"
wait_for_jsonpath "project/demo-prod" "{.status.phase}" "Ready"
wait_for_jsonpath "serviceclass/namespace" "{.status.phase}" "Ready"
wait_for_jsonpath "serviceplan/namespace-team" "{.status.phase}" "Ready"

kubectl delete "serviceinstance/${INSTANCE_NAME}" --ignore-not-found >/dev/null
kubectl delete "namespace/demo-prod-${INSTANCE_NAME}" --ignore-not-found >/dev/null

curl -fsS -X POST "http://${BFF_ADDR}/api/requests" \
  -c "${COOKIE_JAR}" -b "${COOKIE_JAR}" \
  -H "Content-Type: application/json" \
  --data "{\"name\":\"${INSTANCE_NAME}\",\"projectName\":\"demo-prod\",\"serviceClass\":\"namespace\",\"servicePlan\":\"namespace-team\"}" >/dev/null

wait_for_jsonpath "serviceinstance/${INSTANCE_NAME}" "{.status.phase}" "Provisioning"
artifact_path="$(kubectl get "serviceinstance/${INSTANCE_NAME}" -o jsonpath='{.status.artifact.path}')"
if [[ -z "${artifact_path}" ]]; then
  echo "service instance did not publish an artifact path" >&2
  exit 1
fi
kubectl apply -f "${DELIVERY_ROOT}/${artifact_path}/namespace.yaml"
kubectl apply -f "${DELIVERY_ROOT}/${artifact_path}"
kubectl annotate "serviceinstance/${INSTANCE_NAME}" "servicer.io/demo-refresh=$(date +%s)" --overwrite >/dev/null
wait_for_jsonpath "serviceinstance/${INSTANCE_NAME}" "{.status.phase}" "Ready"

curl -fsS -X POST "http://${BFF_ADDR}/api/instances/${INSTANCE_NAME}/actions" \
  -c "${COOKIE_JAR}" -b "${COOKIE_JAR}" \
  -H "Content-Type: application/json" \
  --data '{"action":"update-quota","reason":"KIND demo smoke test","parameters":{"cpu":"2","memory":"4Gi","pods":"20"}}' >/dev/null

curl -fsS -b "${COOKIE_JAR}" "http://${BFF_ADDR}/api/instances/${INSTANCE_NAME}" >/dev/null
curl -fsS -b "${COOKIE_JAR}" "http://${BFF_ADDR}/api/audit?q=update-quota" >/dev/null

echo "KIND demo smoke test passed for ${INSTANCE_NAME} in ${CLUSTER_NAME}."
