#!/usr/bin/env bash
set -euo pipefail

SOURCE_CLUSTER="${SOURCE_CLUSTER:-servicer-backup-source}"
RESTORE_CLUSTER="${RESTORE_CLUSTER:-servicer-backup-restore}"
KIND_IMAGE="${KIND_IMAGE:-kindest/node:v1.32.2}"
BACKUP_DIR="${BACKUP_DIR:-$(pwd)/.e2e/backup}"

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
  local timeout="${4:-120}"
  local start
  start="$(date +%s)"
  while true; do
    value="$(kubectl get "${resource}" -o "jsonpath=${jsonpath}" 2>/dev/null || true)"
    if [[ "${value}" == "${expected}" ]]; then
      return 0
    fi
    if (( "$(date +%s)" - start > timeout )); then
      echo "timed out waiting for ${resource} ${jsonpath}; last value: ${value}" >&2
      return 1
    fi
    sleep 2
  done
}

create_cluster() {
  local cluster="$1"
  if ! kind get clusters | grep -qx "${cluster}"; then
    kind create cluster --name "${cluster}" --image "${KIND_IMAGE}"
  fi
  kubectl config use-context "kind-${cluster}" >/dev/null
  kubectl apply -f config/crd/bases
  kubectl wait --for=condition=Established crd --all --timeout=90s
}

require kind
require kubectl
require go
require python3
require tar

rm -rf "${BACKUP_DIR}"
mkdir -p "$(dirname "${BACKUP_DIR}")"

create_cluster "${SOURCE_CLUSTER}"
kubectl apply -k config/samples
go run ./cmd/manager --metrics-bind-address=:0 --health-probe-bind-address=:18082 --delivery-root .e2e/source-delivery &
MANAGER_PID="$!"
trap 'kill "${MANAGER_PID}" >/dev/null 2>&1 || true' EXIT
wait_for_jsonpath "tenant/demo" "{.status.phase}" "Ready"
wait_for_jsonpath "project/demo-prod" "{.status.phase}" "Ready"
wait_for_jsonpath "serviceclass/namespace" "{.status.phase}" "Ready"
wait_for_jsonpath "serviceplan/namespace-team" "{.status.phase}" "Ready"
kubectl apply -f - <<'EOF'
apiVersion: platform.servicer.io/v1alpha1
kind: ServiceInstance
metadata:
  name: backup-namespace
spec:
  projectRef:
    name: demo-prod
  serviceClassRef:
    name: namespace
  servicePlanRef:
    name: namespace-team
EOF
wait_for_jsonpath "serviceinstance/backup-namespace" "{.status.artifact.count}" "3"
./hack/control-plane-backup.sh backup "${BACKUP_DIR}"
kill "${MANAGER_PID}" >/dev/null 2>&1 || true
trap - EXIT

create_cluster "${RESTORE_CLUSTER}"
./hack/control-plane-backup.sh restore "${BACKUP_DIR}"
kubectl get tenant/demo >/dev/null
kubectl get project/demo-prod >/dev/null
kubectl get serviceinstance/backup-namespace >/dev/null

echo "Backup/restore e2e passed using ${BACKUP_DIR}."
