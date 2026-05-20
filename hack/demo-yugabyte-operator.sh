#!/usr/bin/env bash
set -euo pipefail

YUGABYTE_OPERATOR_REF="${YUGABYTE_OPERATOR_REF:-main}"
YUGABYTE_DEFAULT_USER="${YUGABYTE_DEFAULT_USER:-servicer-demo}"
YUGABYTE_DEFAULT_EMAIL="${YUGABYTE_DEFAULT_EMAIL:-servicer-demo@example.com}"
YUGABYTE_DEFAULT_PASSWORD="${YUGABYTE_DEFAULT_PASSWORD:-ServicerDemo123!}"
YUGABYTE_OPERATOR_NAMESPACE="${YUGABYTE_OPERATOR_NAMESPACE:-yugabyte-system}"
YUGABYTE_OPERATOR_IMAGE="${YUGABYTE_OPERATOR_IMAGE:-quay.io/yugabyte/yugabyte-k8s-operator:0.1.6}"

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require kubectl
require curl
require git
require helm

workdir="$(mktemp -d)"
cleanup() {
  rm -rf "${workdir}"
}
trap cleanup EXIT

kubectl create namespace "${YUGABYTE_OPERATOR_NAMESPACE}" --dry-run=client -o yaml \
  | kubectl label --local -f - platform.mnorris.dev/profile=platform --dry-run=client -o yaml \
  | kubectl apply -f - >/dev/null

git clone --depth 1 --branch "${YUGABYTE_OPERATOR_REF}" https://github.com/yugabyte/yugabyte-k8s-operator "${workdir}/yugabyte-k8s-operator" >/dev/null
kubectl apply -f "${workdir}/yugabyte-k8s-operator/crd/concatenated_crd.yaml" >/dev/null
kubectl wait --for=condition=Established crd/ybuniverses.operator.yugabyte.io --timeout=90s

helm upgrade --install yugabyte-k8s-operator "${workdir}/yugabyte-k8s-operator/chart" \
  --namespace "${YUGABYTE_OPERATOR_NAMESPACE}" \
  --wait \
  --timeout 10m \
  --set rbac.create=true \
  --set yugaware.service.type=ClusterIP \
  --set yugaware.resources.requests.cpu=500m \
  --set yugaware.resources.requests.memory=1Gi \
  --set postgres.resources.requests.cpu=250m \
  --set postgres.resources.requests.memory=512Mi \
  --set prometheus.resources.requests.cpu=250m \
  --set prometheus.resources.requests.memory=512Mi \
  --set yugaware.kubernetesOperatorNamespace="${YUGABYTE_OPERATOR_NAMESPACE}" \
  --set yugaware.defaultUser.enabled=false \
  --set yugaware.defaultUser.username="${YUGABYTE_DEFAULT_USER}" \
  --set yugaware.defaultUser.email="${YUGABYTE_DEFAULT_EMAIL}" \
  --set yugaware.defaultUser.password="${YUGABYTE_DEFAULT_PASSWORD}" >/dev/null

kubectl run yba-customer-create \
  --rm -i --restart=Never \
  --namespace "${YUGABYTE_OPERATOR_NAMESPACE}" \
  --image="${YUGABYTE_OPERATOR_IMAGE}" \
  --env="YBA_EMAIL=${YUGABYTE_DEFAULT_EMAIL}" \
  --env="YBA_PASSWORD=${YUGABYTE_DEFAULT_PASSWORD}" \
  --env="YBA_NAME=${YUGABYTE_DEFAULT_USER}" \
  --command -- sh -ec '
    payload="{\"email\":\"${YBA_EMAIL}\",\"password\":\"${YBA_PASSWORD}\",\"code\":\"operator\",\"name\":\"${YBA_NAME}\"}"
    code="$(curl -sS -o /tmp/yba-register-response -w "%{http_code}" \
      -X POST \
      --url http://yugabyte-k8s-operator-yugaware-ui/api/register \
      --header "Content-Type: application/json" \
      --data "${payload}" || true)"
    cat /tmp/yba-register-response
    case "${code}" in
      200|201|409) exit 0 ;;
      400)
        grep -Eiq "already|exist|registered|customer|multiple accounts|single tenancy" /tmp/yba-register-response
        ;;
      *) exit 1 ;;
    esac
  ' >/dev/null

echo "YugabyteDB operator and default YBA customer are ready in ${YUGABYTE_OPERATOR_NAMESPACE}."
