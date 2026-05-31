#!/usr/bin/env bash
set -euo pipefail

manifest="${1:-deploy}"
rendered="$(mktemp)"
trap 'rm -f "$rendered"' EXIT

kubectl kustomize "$manifest" > "$rendered"

check_no_rbac_wildcards() {
  local file="$1"
  local label="$2"
  if awk '
    BEGIN { inrole = 0; bad = 0 }
    /^kind:[[:space:]]*(ClusterRole|Role)$/ { inrole = 1 }
    /^---$/ { inrole = 0 }
    inrole {
      if ($0 ~ /apiGroups:[[:space:]]*\[[^]]*\*[^]]*\]/ ||
          $0 ~ /resources:[[:space:]]*\[[^]]*\*[^]]*\]/ ||
          $0 ~ /verbs:[[:space:]]*\[[^]]*\*[^]]*\]/ ||
          $0 ~ /^[[:space:]]*-[[:space:]]*"\*"[[:space:]]*$/ ||
          $0 ~ /^[[:space:]]*-[[:space:]]*\*[[:space:]]*$/) {
        print $0
        bad = 1
      }
    }
    END { exit bad }
  ' "$file" >/tmp/servicer-rbac-wildcards.$$; then
    rm -f /tmp/servicer-rbac-wildcards.$$
    return 0
  fi
  echo "$label contains wildcard RBAC entries:" >&2
  cat /tmp/servicer-rbac-wildcards.$$ >&2
  rm -f /tmp/servicer-rbac-wildcards.$$
  return 1
}

check_no_rbac_wildcards "$rendered" "$manifest"

# Ensure compatibility manifests also stay wildcard-free.
if [[ "$manifest" == "deploy" ]] && [[ -d config/deploy ]]; then
  rendered_compat="$(mktemp)"
  trap 'rm -f "$rendered" "$rendered_compat"' EXIT
  kubectl kustomize config/deploy > "$rendered_compat"
  check_no_rbac_wildcards "$rendered_compat" "config/deploy"
fi

extract_cluster_role() {
  local role_name="$1"
  awk -v RS='---' -v role="$role_name" '
    $0 ~ /kind:[[:space:]]*ClusterRole([[:space:]]|$)/ && $0 !~ /kind:[[:space:]]*ClusterRoleBinding/ && $0 ~ ("name:[[:space:]]*" role "([[:space:]]|$)") { print }
  ' "$rendered"
}

bff_write_block="$(extract_cluster_role "servicer-bff-write")"
if [[ -z "$bff_write_block" ]]; then
  echo "expected ClusterRole servicer-bff-write in rendered $manifest manifest" >&2
  exit 1
fi

if grep -Eq 'apiGroups:[[:space:]]*\["(rbac.authorization.k8s.io|apps|batch|argoproj.io|external-secrets.io|admissionregistration.k8s.io)"\]' <<<"$bff_write_block"; then
  echo "servicer-bff-write must not grant write access to RBAC/workload/operator API groups" >&2
  exit 1
fi

if grep -Eq '(^|[[:space:]-])(namespaces|pods|serviceaccounts|resourcequotas|events)([[:space:],]|$)' <<<"$bff_write_block"; then
  echo "servicer-bff-write includes unexpected high-risk core write resources" >&2
  exit 1
fi

if ! grep -Eq '^[[:space:]]*-[[:space:]]*configmaps[[:space:]]*$' <<<"$bff_write_block" || ! grep -Eq '^[[:space:]]*-[[:space:]]*secrets[[:space:]]*$' <<<"$bff_write_block"; then
  echo "servicer-bff-write must only mutate core configmaps/secrets" >&2
  exit 1
fi
