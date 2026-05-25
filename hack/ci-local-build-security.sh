#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: hack/ci-local-build-security.sh [options]

Builds Servicer container images locally and runs Trivy scans to mirror the CI
build + release-security image checks before pushing.

Options:
  --components <csv>    Components to build/scan.
                        Allowed: manager,bff,syncer,web,tools
                        Default: manager,bff,syncer,web,tools
  --image-prefix <name> Image prefix (without component suffix).
                        Default: servicer-local
  --tag <tag>           Image tag for local builds.
                        Default: ci-local
  --platform <platform> Build platform.
                        Default: linux/amd64
  --severity <levels>   Trivy severities (CSV).
                        Default: CRITICAL,HIGH
  --skip-validate       Skip validate-stage checks and run only build+image scan.
  -h, --help            Show this help.

Environment:
  TRIVY_IMAGE      Trivy image used when local `trivy` is not installed.
                   Default: aquasec/trivy:latest
  TRIVY_CACHE_DIR  Cache directory for Trivy DB.
                   Default: $HOME/.cache/trivy
EOF
}

log() {
  printf '==> %s\n' "$*"
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

have_cmd() {
  command -v "$1" >/dev/null 2>&1
}

require_cmd() {
  have_cmd "$1" || die "required command not found: $1"
}

dockerfile_for_component() {
  case "$1" in
    manager) printf 'Containerfile.manager' ;;
    bff) printf 'Containerfile.bff' ;;
    syncer) printf 'Containerfile.syncer' ;;
    web) printf 'Containerfile.web' ;;
    tools) printf 'Containerfile.tools' ;;
    *) return 1 ;;
  esac
}

component_is_valid() {
  case "$1" in
    manager|bff|syncer|web|tools) return 0 ;;
    *) return 1 ;;
  esac
}

run_trivy() {
  if have_cmd trivy; then
    trivy "$@"
    return
  fi

  require_cmd docker
  mkdir -p "${TRIVY_CACHE_DIR}"
  docker run --rm \
    -e TRIVY_DISABLE_VEX_NOTICE="${TRIVY_DISABLE_VEX_NOTICE:-1}" \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v "${TRIVY_CACHE_DIR}:/root/.cache/" \
    -v "${REPO_ROOT}:${REPO_ROOT}" \
    -w "${REPO_ROOT}" \
    "${TRIVY_IMAGE}" "$@"
}

run_validate_checks() {
  log "Running validate checks (matching CI validate job)"
  require_cmd go
  require_cmd npm
  require_cmd kubectl

  go mod verify
  go test -race -coverprofile=coverage.out ./...
  go run golang.org/x/vuln/cmd/govulncheck@latest ./...
  go run github.com/securego/gosec/v2/cmd/gosec@latest ./...
  run_trivy fs --scanners vuln --severity "${SEVERITY}" --exit-code 1 .

  (
    cd web
    npm ci
    npm run build
    npm audit --omit=dev --audit-level=high
  )

  kubectl kustomize config/deploy > /dev/null
  kubectl kustomize deploy > /dev/null
  kubectl kustomize config/samples > /dev/null
  ./hack/manifest-policy.sh deploy
}

run_image_build_and_scan() {
  require_cmd docker
  docker buildx version >/dev/null 2>&1 || die "docker buildx is required"
  docker info >/dev/null 2>&1 || die "docker daemon is not reachable"

  for component in "${SELECTED_COMPONENTS[@]}"; do
    local dockerfile image
    dockerfile="$(dockerfile_for_component "${component}")" || die "unknown component: ${component}"
    image="${IMAGE_PREFIX}-${component}:${TAG}"

    log "Building ${component} image (${image})"
    docker buildx build \
      --platform "${PLATFORM}" \
      --file "${dockerfile}" \
      --tag "${image}" \
      --load \
      .

    log "Scanning ${image} with Trivy"
    run_trivy image --scanners vuln --severity "${SEVERITY}" --exit-code 1 "${image}"
  done
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

IMAGE_PREFIX="${IMAGE_PREFIX:-servicer-local}"
TAG="${TAG:-ci-local}"
PLATFORM="${PLATFORM:-linux/amd64}"
SEVERITY="${SEVERITY:-CRITICAL,HIGH}"
TRIVY_IMAGE="${TRIVY_IMAGE:-aquasec/trivy:latest}"
TRIVY_CACHE_DIR="${TRIVY_CACHE_DIR:-${HOME}/.cache/trivy}"
RUN_VALIDATE=1
SELECTED_COMPONENTS=(manager bff syncer web tools)

while [[ $# -gt 0 ]]; do
  case "$1" in
    --components)
      [[ $# -ge 2 ]] || die "--components requires a value"
      IFS=',' read -r -a SELECTED_COMPONENTS <<< "$2"
      shift 2
      ;;
    --image-prefix)
      [[ $# -ge 2 ]] || die "--image-prefix requires a value"
      IMAGE_PREFIX="$2"
      shift 2
      ;;
    --tag)
      [[ $# -ge 2 ]] || die "--tag requires a value"
      TAG="$2"
      shift 2
      ;;
    --platform)
      [[ $# -ge 2 ]] || die "--platform requires a value"
      PLATFORM="$2"
      shift 2
      ;;
    --severity)
      [[ $# -ge 2 ]] || die "--severity requires a value"
      SEVERITY="$2"
      shift 2
      ;;
    --skip-validate)
      RUN_VALIDATE=0
      shift
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

[[ "${#SELECTED_COMPONENTS[@]}" -gt 0 ]] || die "no components selected"
for component in "${SELECTED_COMPONENTS[@]}"; do
  component_is_valid "${component}" || die "invalid component: ${component}"
done

if [[ "${RUN_VALIDATE}" -eq 1 ]]; then
  run_validate_checks
fi
run_image_build_and_scan

log "All selected components built and scanned successfully."
