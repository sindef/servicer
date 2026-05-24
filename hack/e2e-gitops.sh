#!/usr/bin/env bash
set -euo pipefail

CLUSTER="${CLUSTER:-servicer-gitops-e2e}"
KIND_IMAGE="${KIND_IMAGE:-kindest/node:v1.32.2}"
DELIVERY_REMOTE="${DELIVERY_REMOTE:-$(pwd)/.e2e/servicer-delivery.git}"
DELIVERY_WORKTREE="${DELIVERY_WORKTREE:-$(pwd)/.e2e/servicer-delivery}"
DELIVERY_ROOT="${DELIVERY_ROOT:-$(pwd)/.e2e/generated}"
INSTANCE_NAME="${INSTANCE_NAME:-gitops-namespace}"

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

require kind
require kubectl
require git
require go

mkdir -p .e2e
rm -rf "${DELIVERY_REMOTE}" "${DELIVERY_WORKTREE}" "${DELIVERY_ROOT}"
git init --bare "${DELIVERY_REMOTE}"
git clone "${DELIVERY_REMOTE}" "${DELIVERY_WORKTREE}"
git -C "${DELIVERY_WORKTREE}" config user.email servicer@example.com
git -C "${DELIVERY_WORKTREE}" config user.name Servicer
touch "${DELIVERY_WORKTREE}/README.md"
git -C "${DELIVERY_WORKTREE}" add README.md
git -C "${DELIVERY_WORKTREE}" commit -m "seed delivery repo"
git -C "${DELIVERY_WORKTREE}" branch -M main
git -C "${DELIVERY_WORKTREE}" push origin main
rm -rf "${DELIVERY_WORKTREE}"

if ! kind get clusters | grep -qx "${CLUSTER}"; then
  kind create cluster --name "${CLUSTER}" --image "${KIND_IMAGE}"
fi
kubectl config use-context "kind-${CLUSTER}" >/dev/null
kubectl apply -f config/crd/bases
kubectl wait --for=condition=Established crd --all --timeout=90s
kubectl apply -k config/samples
kubectl create namespace argocd --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/v2.13.3/manifests/install.yaml
kubectl wait -n argocd --for=condition=Available deploy/argocd-repo-server --timeout=180s

go test ./internal/deliveryrepo ./internal/controllers

go run ./cmd/manager \
  --metrics-bind-address=:0 \
  --health-probe-bind-address=:18081 \
  --delivery-root "${DELIVERY_ROOT}" \
  --delivery-repo-url "file://${DELIVERY_REMOTE}" \
  --delivery-repo-ref main \
  --delivery-repo-path generated/delivery \
  --delivery-repo-worktree "${DELIVERY_WORKTREE}" \
  --delivery-repo-auto-commit=true \
  --delivery-repo-auto-push=true \
  --delivery-repo-branch main \
  --argocd-namespace argocd &
MANAGER_PID="$!"
trap 'kill "${MANAGER_PID}" >/dev/null 2>&1 || true' EXIT

wait_for_jsonpath "tenant/demo" "{.status.phase}" "Ready"
wait_for_jsonpath "project/demo-prod" "{.status.phase}" "Ready"
wait_for_jsonpath "serviceclass/namespace" "{.status.phase}" "Ready"
wait_for_jsonpath "serviceplan/namespace-team" "{.status.phase}" "Ready"

kubectl delete "serviceinstance/${INSTANCE_NAME}" --ignore-not-found >/dev/null
kubectl apply -f - <<EOF
apiVersion: platform.servicer.io/v1alpha1
kind: ServiceInstance
metadata:
  name: ${INSTANCE_NAME}
spec:
  projectRef:
    name: demo-prod
  serviceClassRef:
    name: namespace
  servicePlanRef:
    name: namespace-team
EOF

wait_for_jsonpath "serviceinstance/${INSTANCE_NAME}" "{.status.artifact.count}" "3"
commit="$(git --git-dir "${DELIVERY_REMOTE}" rev-parse refs/heads/main)"
if [[ -z "${commit}" ]]; then
  echo "delivery repo did not receive a commit" >&2
  exit 1
fi
APP_NAME="demo-prod-${INSTANCE_NAME}"
kubectl get -n argocd "application/${APP_NAME}" >/dev/null
source_path="$(kubectl get -n argocd "application/${APP_NAME}" -o jsonpath='{.spec.source.path}')"
if [[ "${source_path}" != generated/delivery/*"${INSTANCE_NAME}" ]]; then
  echo "unexpected Argo CD source path: ${source_path}" >&2
  exit 1
fi

echo "GitOps e2e passed: ServiceInstance published commit ${commit} and created Argo CD Application ${APP_NAME}."
