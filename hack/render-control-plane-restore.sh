#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF' >&2
Usage: hack/render-control-plane-restore.sh <backup-path>

Renders the opt-in control-plane restore Job (config/backup/restore) with
BACKUP_PATH set to a real backup directory on the servicer-control-plane-backups
PVC.

  <backup-path>  Absolute single-segment directory under /backups, e.g.
                 /backups/servicer-20260101-021700

Pipe the output to kubectl apply -f - to create the one-shot restore Job.
EOF
}

die() {
  echo "error: $*" >&2
  exit 1
}

if [[ $# -ne 1 ]]; then
  usage
  exit 1
fi

BACKUP_PATH="$1"

[[ -n "${BACKUP_PATH}" ]] || die "BACKUP_PATH is empty"
[[ "${BACKUP_PATH}" != *'__BACKUP_PATH__'* ]] || die "BACKUP_PATH is still the placeholder"
case "${BACKUP_PATH}" in
  *YYYYMMDD* | *HHMMSS*)
    die "BACKUP_PATH still contains the timestamp placeholder"
    ;;
esac
case "${BACKUP_PATH}" in
  /backups/*) ;;
  *) die "BACKUP_PATH must live under /backups (got: ${BACKUP_PATH})" ;;
esac
case "${BACKUP_PATH}" in
  *'../'* | *'/..' | *'./'* | *'/.')
    die "BACKUP_PATH must not contain relative segments"
    ;;
esac
[[ "${BACKUP_PATH}" =~ ^/backups/[A-Za-z0-9._-]+$ ]] ||
  die "BACKUP_PATH must be a single /backups/<directory> path (got: ${BACKUP_PATH})"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

kubectl kustomize "${REPO_ROOT}/config/backup/restore" |
  sed "s#__BACKUP_PATH__#${BACKUP_PATH}#g"
