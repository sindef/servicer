#!/usr/bin/env bash
set -euo pipefail

manifest="${1:-deploy}"
rendered="$(mktemp)"
trap 'rm -f "$rendered"' EXIT

kubectl kustomize "$manifest" > "$rendered"

if grep -Eq 'image: .*:latest($|[[:space:]])' "$rendered"; then
  echo "production manifests must not use mutable latest image tags" >&2
  exit 1
fi

if grep -Eq 'resources:[[:space:]]*\[[[:space:]]*"?\*"?[[:space:]]*\]' "$rendered"; then
  echo "production RBAC must not use wildcard resources" >&2
  exit 1
fi

if grep -Eq 'verbs:[[:space:]]*\[[[:space:]]*"?\*"?[[:space:]]*\]' "$rendered"; then
  echo "production RBAC must not use wildcard verbs" >&2
  exit 1
fi

if grep -Eq 'apiGroups:[[:space:]]*\[[[:space:]]*"?\*"?[[:space:]]*\]' "$rendered"; then
  echo "production RBAC must not use wildcard apiGroups" >&2
  exit 1
fi

required=(
  'kind: PodDisruptionBudget'
  'kind: NetworkPolicy'
  'readOnlyRootFilesystem: true'
  'seccompProfile:'
  'livenessProbe:'
  'readinessProbe:'
  'limits:'
  'requests:'
  '--leader-elect=true'
  '--metrics-bind-address=:8080'
)

for pattern in "${required[@]}"; do
  if ! grep -Fq -- "$pattern" "$rendered"; then
    echo "production manifest missing required policy marker: $pattern" >&2
    exit 1
  fi
done

./hack/rbac-policy.sh "$manifest"
