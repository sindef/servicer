#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: hack/helm-cli-smoke.sh [--helm-version <version>]

Runs a Helm CLI smoke test using the same command pattern used by the manager:
  - helm dependency build <chartPath>
  - helm template <release> <chartPath> --namespace <ns> --include-crds --set k=v

If --helm-version is omitted, the version is read from Containerfile.manager
(`ARG HELM_VERSION=...`).
EOF
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

HELM_VERSION=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --helm-version)
      [[ $# -ge 2 ]] || die "--helm-version requires a value"
      HELM_VERSION="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      die "unknown argument: $1"
      ;;
  esac
done

if [[ -z "${HELM_VERSION}" ]]; then
  HELM_VERSION="$(awk -F= '/^ARG HELM_VERSION=/{print $2; exit}' "${REPO_ROOT}/Containerfile.manager")"
fi
[[ -n "${HELM_VERSION}" ]] || die "unable to determine HELM_VERSION"

require_cmd curl
require_cmd tar
require_cmd sha256sum

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

helm_tar="${workdir}/helm-${HELM_VERSION}-linux-amd64.tar.gz"
helm_sum="${workdir}/helm.tgz.sha256sum"

curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz" -o "${helm_tar}"
curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz.sha256sum" -o "${helm_sum}"

(
  cd "${workdir}"
  grep "helm-${HELM_VERSION}-linux-amd64.tar.gz" "${helm_sum}" | sha256sum -c -
)

tar -xzf "${helm_tar}" -C "${workdir}"
helm_bin="${workdir}/linux-amd64/helm"
[[ -x "${helm_bin}" ]] || die "helm binary not found after extraction"

"${helm_bin}" create "${workdir}/chart" >/dev/null
"${helm_bin}" dependency build "${workdir}/chart" >/dev/null
"${helm_bin}" template servicer-smoke "${workdir}/chart" \
  --namespace operators \
  --include-crds \
  --set image.tag=smoke >/dev/null

echo "Helm smoke check passed for ${HELM_VERSION}"
