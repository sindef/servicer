#!/usr/bin/env bash
set -euo pipefail

rendered="$(mktemp)"
trap 'rm -f "${rendered}"' EXIT

kubectl kustomize deploy > "${rendered}"

count="$(grep -c '^kind: NetworkPolicy$' "${rendered}" || true)"
if [[ "${count}" -lt 4 ]]; then
  echo "expected at least 4 NetworkPolicy resources in deploy manifests, found ${count}" >&2
  exit 1
fi

grep -q 'name: default-deny' "${rendered}" || {
  echo "missing default-deny NetworkPolicy in deploy manifests" >&2
  exit 1
}

grep -q 'namespace: servicer-system' "${rendered}" || {
  echo "expected servicer-system namespace in NetworkPolicy manifests" >&2
  exit 1
}

echo "NetworkPolicy manifest smoke passed (${count} policies rendered)."
