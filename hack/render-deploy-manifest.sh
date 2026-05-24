#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF' >&2
Usage: hack/render-deploy-manifest.sh <version>

Environment:
  IMAGE_PREFIX  Base image prefix without the component suffix.
                Default: ghcr.io/<git-remote-owner>/servicer
  NAMESPACE     Namespace to render into.
                Default: servicer-system
EOF
}

if [[ $# -ne 1 ]]; then
  usage
  exit 1
fi

VERSION="$1"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
NAMESPACE="${NAMESPACE:-servicer-system}"

default_image_prefix() {
  local remote owner
  remote="$(git -C "${REPO_ROOT}" config --get remote.origin.url 2>/dev/null || true)"
  if [[ "${remote}" =~ github\.com[:/]([^/]+)/([^/.]+)(\.git)?$ ]]; then
    owner="${BASH_REMATCH[1]}"
    printf 'ghcr.io/%s/servicer' "${owner}"
    return
  fi
  printf 'ghcr.io/sindef/servicer'
}

IMAGE_PREFIX="${IMAGE_PREFIX:-$(default_image_prefix)}"

kubectl kustomize "${REPO_ROOT}/deploy" \
  | sed \
    -e "s#ghcr.io/sindef/servicer-manager:v0.1.0#${IMAGE_PREFIX}-manager:${VERSION}#g" \
    -e "s#ghcr.io/sindef/servicer-bff:v0.1.0#${IMAGE_PREFIX}-bff:${VERSION}#g" \
    -e "s#ghcr.io/sindef/servicer-web:v0.1.0#${IMAGE_PREFIX}-web:${VERSION}#g" \
    -e "s#ghcr.io/sindef/servicer-tools:v0.1.0#${IMAGE_PREFIX}-tools:${VERSION}#g" \
    -e "s#servicer-system#${NAMESPACE}#g"
