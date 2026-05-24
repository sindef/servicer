#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  ./hack/control-plane-backup.sh backup [output-dir]
  ./hack/control-plane-backup.sh restore <input-dir>

What it does:
  - exports Servicer cluster-scoped CRDs as sanitized JSON manifests
  - exports servicer-system namespace resources as sanitized JSON manifests
  - exports Servicer-managed Argo CD Applications
  - snapshots local generated/ delivery tree when present

Notes:
  - backs up Servicer control-plane objects and selected runtime state
  - not full etcd backup
  - does not replace cluster/etcd backup for workloads, PVs, node state, or third-party operators
  - Git-backed delivery repositories are the source of truth for production artifacts
  - requires: kubectl, python3, tar
EOF
}

need_bin() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required binary: $1" >&2
    exit 1
  }
}

sanitize_json() {
  python3 - <<'PY'
import json, sys

DROP_META = {
    "creationTimestamp",
    "deletionGracePeriodSeconds",
    "deletionTimestamp",
    "generation",
    "managedFields",
    "resourceVersion",
    "selfLink",
    "uid",
}

def clean(obj):
    if isinstance(obj, dict):
        obj = dict(obj)
        meta = obj.get("metadata")
        if isinstance(meta, dict):
            for key in list(meta.keys()):
                if key in DROP_META:
                    meta.pop(key, None)
            if not meta.get("annotations"):
                meta.pop("annotations", None)
        obj.pop("status", None)
        return {k: clean(v) for k, v in obj.items()}
    if isinstance(obj, list):
        return [clean(v) for v in obj]
    return obj

payload = json.load(sys.stdin)
if isinstance(payload, dict) and payload.get("kind") == "List":
    items = [clean(item) for item in payload.get("items", [])]
    payload = {"apiVersion": "v1", "kind": "List", "items": items}
else:
    payload = clean(payload)
json.dump(payload, sys.stdout, indent=2, sort_keys=True)
sys.stdout.write("\n")
PY
}

backup_resources() {
  local output_dir="$1"
  local cluster_dir="$output_dir/cluster"
  local ns_dir="$output_dir/namespaces/servicer-system"
  local argo_dir="$output_dir/namespaces/argocd"
  mkdir -p "$cluster_dir" "$ns_dir" "$argo_dir"

  local cluster_resources=(
    tenants.platform.servicer.io
    projects.platform.servicer.io
    clustertargets.platform.servicer.io
    authproviders.platform.servicer.io
    users.platform.servicer.io
    groups.platform.servicer.io
    rolebindings.platform.servicer.io
    operatorpackages.platform.servicer.io
    policies.platform.servicer.io
    serviceclasses.platform.servicer.io
    serviceplans.platform.servicer.io
    serviceinstances.platform.servicer.io
    namespaceclaims.platform.servicer.io
    servicebindings.platform.servicer.io
    virtualmachineclaims.platform.servicer.io
    actionrequests.platform.servicer.io
  )

  for resource in "${cluster_resources[@]}"; do
    echo "exporting $resource"
    kubectl get "$resource" -o json --ignore-not-found | sanitize_json >"$cluster_dir/${resource//./-}.json"
  done

  local namespaced_resources=(
    secrets
    configmaps
    serviceaccounts
    roles.rbac.authorization.k8s.io
    rolebindings.rbac.authorization.k8s.io
  )
  for resource in "${namespaced_resources[@]}"; do
    echo "exporting servicer-system/$resource"
    kubectl -n servicer-system get "$resource" -o json --ignore-not-found | sanitize_json >"$ns_dir/${resource//./-}.json"
  done

  echo "exporting argocd Applications managed by Servicer"
  kubectl -n argocd get applications.argoproj.io -l servicer.io/managed-by=servicer -o json --ignore-not-found | sanitize_json >"$argo_dir/applications-argoproj-io.json" || true
  kubectl -n argocd get applicationsets.argoproj.io -l servicer.io/managed-by=servicer -o json --ignore-not-found | sanitize_json >"$argo_dir/applicationsets-argoproj-io.json" || true
}

snapshot_generated_tree() {
  local output_dir="$1"
  if [[ -d generated ]]; then
    tar -czf "$output_dir/generated-delivery.tgz" generated
  fi
}

write_metadata() {
  local output_dir="$1"
  cat >"$output_dir/metadata.env" <<EOF
BACKUP_CREATED_AT=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
KUBECTL_CONTEXT=$(kubectl config current-context 2>/dev/null || echo unknown)
EOF
}

backup() {
  local output_dir="${1:-./backups/servicer-$(date +%Y%m%d-%H%M%S)}"
  mkdir -p "$output_dir"
  backup_resources "$output_dir"
  snapshot_generated_tree "$output_dir"
  write_metadata "$output_dir"
  echo "backup written to $output_dir"
}

restore_dir() {
  local input_dir="$1"
  [[ -d "$input_dir" ]] || {
    echo "backup dir not found: $input_dir" >&2
    exit 1
  }

  shopt -s nullglob
  for file in "$input_dir"/cluster/*.json; do
    echo "applying $(basename "$file")"
    kubectl apply -f "$file"
  done
  for file in "$input_dir"/namespaces/servicer-system/*.json; do
    echo "applying servicer-system/$(basename "$file")"
    kubectl apply -f "$file"
  done
  for file in "$input_dir"/namespaces/argocd/*.json; do
    echo "applying argocd/$(basename "$file")"
    kubectl apply -f "$file" || true
  done
  if [[ -f "$input_dir/generated-delivery.tgz" ]]; then
    tar -xzf "$input_dir/generated-delivery.tgz"
  fi
  echo "restore completed from $input_dir"
}

main() {
  need_bin kubectl
  need_bin python3
  need_bin tar

  local command="${1:-}"
  case "$command" in
    backup)
      backup "${2:-}"
      ;;
    restore)
      [[ $# -ge 2 ]] || {
        usage
        exit 1
      }
      restore_dir "$2"
      ;;
    *)
      usage
      exit 1
      ;;
  esac
}

main "$@"
