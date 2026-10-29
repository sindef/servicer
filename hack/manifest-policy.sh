#!/usr/bin/env bash
set -euo pipefail

manifest="${1:-deploy}"
rendered="$(mktemp)"
scratch="$(mktemp -d)"
trap 'rm -f "$rendered"; rm -rf "$scratch"' EXIT

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
  # The manager image runs as uid 65532 and the delivery SSH key is a Secret
  # volume projected root:<fsGroup>; without fsGroup the key is unreadable.
  'fsGroup: 65532'
)

for pattern in "${required[@]}"; do
  if ! grep -Fq -- "$pattern" "$rendered"; then
    echo "production manifest missing required policy marker: $pattern" >&2
    exit 1
  fi
done

# The restore Job is opt-in (config/backup/restore): its BACKUP_PATH is only
# known at restore time, so installing it from the base render leaves a
# permanently Failed Job in servicer-system.
if grep -Fq 'name: servicer-control-plane-restore' "$rendered" ||
  grep -Fq 'servicer-YYYYMMDD-HHMMSS' "$rendered"; then
  echo "production manifests must not include the control-plane restore Job template" >&2
  echo "render it with bash hack/render-control-plane-restore.sh <backup-path> when a restore is needed" >&2
  exit 1
fi

restore_path="/backups/servicer-20240101-000000"
kubectl kustomize config/backup/restore >"$scratch/restore-overlay.yaml"
bash ./hack/render-control-plane-restore.sh "$restore_path" >"$scratch/restore-job.yaml"

if ! grep -Fq "value: ${restore_path}" "$scratch/restore-job.yaml"; then
  echo "restore render helper did not substitute BACKUP_PATH" >&2
  exit 1
fi

if grep -Fq 'servicer-YYYYMMDD-HHMMSS' "$scratch/restore-job.yaml"; then
  echo "restore render helper left the placeholder BACKUP_PATH in the Job" >&2
  exit 1
fi

if bash ./hack/render-control-plane-restore.sh /backups/servicer-YYYYMMDD-HHMMSS >/dev/null 2>&1; then
  echo "restore render helper must reject the placeholder BACKUP_PATH" >&2
  exit 1
fi

./hack/rbac-policy.sh "$manifest"
