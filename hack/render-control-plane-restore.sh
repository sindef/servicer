#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF' >&2
Usage: hack/render-control-plane-restore.sh <backup-path>

Renders the opt-in control-plane restore Job (config/backup/restore) with a
real BACKUP_PATH and writes the manifest to stdout.

<backup-path> is a directory on the servicer-control-plane-backups PVC, for
example /backups/servicer-20240513-021700.

Example:
  bash hack/render-control-plane-restore.sh /backups/servicer-20240513-021700 \
    | kubectl apply -f -
EOF
}

if [[ $# -ne 1 ]]; then
  usage
  exit 1
fi

BACKUP_PATH="$1"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE="${SCRIPT_DIR}/../config/backup/restore/control-plane-restore-job.yaml"
PLACEHOLDER="/backups/servicer-YYYYMMDD-HHMMSS"

if [[ ! -f "$TEMPLATE" ]]; then
  echo "restore Job template not found: $TEMPLATE" >&2
  exit 1
fi

if [[ "$BACKUP_PATH" == "$PLACEHOLDER" ]]; then
  echo "refusing to render the restore Job with the template placeholder path" >&2
  echo "pass the real backup directory, for example /backups/servicer-20240513-021700" >&2
  exit 1
fi

# The Job mounts the servicer-control-plane-backups PVC at /backups, so the
# path must be an absolute path inside that mount and safe to inline in YAML.
if [[ ! "$BACKUP_PATH" =~ ^/backups(/[A-Za-z0-9._-]+)+$ ]]; then
  echo "invalid backup path: $BACKUP_PATH" >&2
  echo "expected an absolute path under /backups with no spaces, quotes, or wildcards" >&2
  exit 1
fi

IFS='/' read -r -a segments <<<"$BACKUP_PATH"
for segment in "${segments[@]}"; do
  if [[ "$segment" == "." || "$segment" == ".." ]]; then
    echo "invalid backup path: $BACKUP_PATH" >&2
    echo "relative path segments are not allowed" >&2
    exit 1
  fi
done

sed -e "s|${PLACEHOLDER}|${BACKUP_PATH}|g" "$TEMPLATE"
