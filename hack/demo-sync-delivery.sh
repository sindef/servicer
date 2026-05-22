#!/usr/bin/env bash
set -euo pipefail

DELIVERY_ROOT="${DELIVERY_ROOT:-$(pwd)/generated/demo-delivery}"
INTERVAL_SECONDS="${INTERVAL_SECONDS:-3}"

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

apply_once() {
  if [[ ! -d "${DELIVERY_ROOT}/clusters" ]]; then
    return 0
  fi
  local failures=0
  # Apply operator packages (CRDs, controllers) — cluster-scoped, before services.
  while IFS= read -r -d '' package_dir; do
    if [[ ! -d "${package_dir}" ]]; then
      continue
    fi
    if ! kubectl apply --server-side -f "${package_dir}" >/dev/null; then
      echo "failed applying operator package ${package_dir}" >&2
      failures=$((failures + 1))
    fi
  done < <(find "${DELIVERY_ROOT}/clusters" -path '*/operators/*' -type d -print0 2>/dev/null)
  # Apply service packages (databases, namespaces, etc.)
  while IFS= read -r -d '' package_dir; do
    if [[ ! -d "${package_dir}" ]]; then
      continue
    fi
    if [[ -f "${package_dir}/namespace.yaml" ]]; then
      if ! kubectl apply -f "${package_dir}/namespace.yaml" >/dev/null; then
        echo "failed applying namespace for ${package_dir}" >&2
        failures=$((failures + 1))
        continue
      fi
    fi
    if ! kubectl apply -f "${package_dir}" >/dev/null; then
      echo "failed applying package ${package_dir}" >&2
      failures=$((failures + 1))
    fi
  done < <(find "${DELIVERY_ROOT}/clusters" -path '*/services/*' -type d -print0 2>/dev/null)
  # Apply Argo CD Application manifests
  while IFS= read -r -d '' app_dir; do
    if [[ ! -d "${app_dir}" ]]; then
      continue
    fi
    if ! kubectl apply -f "${app_dir}" >/dev/null; then
      echo "failed applying argo-app ${app_dir}" >&2
      failures=$((failures + 1))
    fi
  done < <(find "${DELIVERY_ROOT}/clusters" -path '*/argo-apps/*' -type d -print0 2>/dev/null)
  return "${failures}"
}

require kubectl

if [[ "${1:-}" == "--once" ]]; then
  apply_once
  exit 0
fi

echo "Syncing generated delivery packages from ${DELIVERY_ROOT} every ${INTERVAL_SECONDS}s."
while true; do
  if [[ -n "${KUBECONFIG:-}" && ! -f "${KUBECONFIG}" ]]; then
    sleep "${INTERVAL_SECONDS}"
    continue
  fi
  apply_once || true
  sleep "${INTERVAL_SECONDS}"
done
