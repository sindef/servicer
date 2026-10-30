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

# Every pod that asks for runAsNonRoot must point at an image whose build sets a
# non-root user. The kubelet refuses a container whose image user is root with
# "container has runAsNonRoot and image will run as root", so a manifest-only
# grep for the marker is not enough: the image build has to agree with it.
python3 "$(dirname "$0")/check-image-users.py" "$rendered"

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

# The base install must not ship the control-plane restore Job. Its command
# fails by design until an operator supplies a real backup directory, so a
# base render containing it leaves a permanently Failed Job after install.
if grep -Fq 'servicer-control-plane-restore' "$rendered"; then
  echo "production manifests must not include the control-plane restore Job" >&2
  exit 1
fi

if grep -Fq '__BACKUP_PATH__' "$rendered"; then
  echo "production manifests must not include the restore BACKUP_PATH placeholder" >&2
  exit 1
fi

# Self-check the opt-in restore render helper: it must refuse the placeholder,
# relative segments, and paths outside /backups before it renders anything.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
for bad_path in '__BACKUP_PATH__' 'relative/path' '/etc'; do
  if bash "${script_dir}/render-control-plane-restore.sh" "${bad_path}" >/dev/null 2>&1; then
    echo "render-control-plane-restore.sh accepted invalid BACKUP_PATH: ${bad_path}" >&2
    exit 1
  fi
done

./hack/rbac-policy.sh "$manifest"
