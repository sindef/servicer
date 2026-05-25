#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${ROOT}/dist/THIRD_PARTY_LICENSES"

sanitize() {
  printf '%s' "$1" | tr '/:@+' '____'
}

copy_license_files() {
  local source_dir="$1"
  local target_dir="$2"
  mkdir -p "${target_dir}"
  find "${source_dir}" -maxdepth 2 -type f \
    \( -iname 'license*' -o -iname 'copying*' -o -iname 'notice*' \) \
    -print0 | while IFS= read -r -d '' file; do
      local rel
      rel="${file#"${source_dir}/"}"
      mkdir -p "${target_dir}/$(dirname "${rel}")"
      cp "${file}" "${target_dir}/${rel}"
    done
}

rm -rf "${OUT}"
mkdir -p "${OUT}/go" "${OUT}/web" "${OUT}/operators"

cp "${ROOT}/LICENSE" "${OUT}/SERVICER-LICENSE"
cp "${ROOT}/THIRD_PARTY_NOTICES.md" "${OUT}/THIRD_PARTY_NOTICES.md"

pushd "${ROOT}" >/dev/null
mod_backup="$(mktemp -d)"
cp go.mod "${mod_backup}/go.mod"
if [[ -f go.sum ]]; then
  cp go.sum "${mod_backup}/go.sum"
fi
restore_mod_files() {
  cp "${mod_backup}/go.mod" go.mod
  if [[ -f "${mod_backup}/go.sum" ]]; then
    cp "${mod_backup}/go.sum" go.sum
  else
    rm -f go.sum
  fi
  rm -rf "${mod_backup}"
}
trap restore_mod_files EXIT
go mod download all
go list -m -f '{{if not .Main}}{{.Path}}{{"\t"}}{{.Version}}{{"\t"}}{{.Dir}}{{end}}' all |
  while IFS=$'\t' read -r module version dir; do
    [[ -n "${module}" && -n "${dir}" && -d "${dir}" ]] || continue
    target="${OUT}/go/$(sanitize "${module}@${version}")"
    copy_license_files "${dir}" "${target}"
    if ! find "${target}" -type f | grep -q .; then
      rmdir "${target}" 2>/dev/null || true
      printf '%s\t%s\t%s\n' "${module}" "${version}" "NO_LICENSE_FILE_FOUND" >>"${OUT}/go/MISSING_LICENSE_FILES.tsv"
    fi
  done
restore_mod_files
trap - EXIT
popd >/dev/null

if [[ ! -d "${ROOT}/web/node_modules" ]]; then
  npm --prefix "${ROOT}/web" ci --ignore-scripts
fi

node - "${ROOT}" "${OUT}" <<'NODE'
const fs = require('fs')
const path = require('path')

const root = process.argv[2]
const out = process.argv[3]
const nodeModules = path.join(root, 'web', 'node_modules')
const summary = []
const missing = []

function packages(dir) {
  if (!fs.existsSync(dir)) return []
  const entries = fs.readdirSync(dir, { withFileTypes: true })
  const found = []
  for (const entry of entries) {
    if (!entry.isDirectory() || entry.name.startsWith('.')) continue
    const full = path.join(dir, entry.name)
    if (entry.name.startsWith('@')) {
      found.push(...packages(full))
      continue
    }
    const manifest = path.join(full, 'package.json')
    if (fs.existsSync(manifest)) found.push(full)
  }
  return found
}

function sanitize(value) {
  return value.replace(/[\/:@+]/g, '_')
}

function copyRecursive(src, dest) {
  fs.mkdirSync(path.dirname(dest), { recursive: true })
  fs.copyFileSync(src, dest)
}

for (const dir of packages(nodeModules)) {
  const manifest = JSON.parse(fs.readFileSync(path.join(dir, 'package.json'), 'utf8'))
  if (manifest.private === true) continue
  const name = manifest.name || path.basename(dir)
  const version = manifest.version || 'unknown'
  const license = typeof manifest.license === 'string' ? manifest.license : JSON.stringify(manifest.license || '')
  const target = path.join(out, 'web', sanitize(`${name}@${version}`))
  summary.push([name, version, license || 'UNKNOWN'].join('\t'))

  let copied = 0
  for (const file of fs.readdirSync(dir)) {
    if (/^(licen[sc]e|copying|notice)/i.test(file)) {
      copyRecursive(path.join(dir, file), path.join(target, file))
      copied++
    }
  }
  if (copied === 0) missing.push([name, version, license || 'UNKNOWN'].join('\t'))
}

fs.writeFileSync(path.join(out, 'web', 'PACKAGE_LICENSES.tsv'), summary.sort().join('\n') + '\n')
if (missing.length) {
  fs.writeFileSync(path.join(out, 'web', 'MISSING_LICENSE_FILES.tsv'), missing.sort().join('\n') + '\n')
}
NODE

cat >"${OUT}/operators/README.md" <<'EOF'
# Operator and service license sources

This directory records externally installed or referenced operator/service
software. Servicer does not vendor these projects into its source tree.

- CloudNativePG: Apache-2.0, https://github.com/cloudnative-pg/cloudnative-pg
- External Secrets Operator: Apache-2.0, https://github.com/external-secrets/external-secrets
- Valkey: BSD-3-Clause, https://github.com/valkey-io/valkey
- YugabyteDB core: Apache-2.0, https://github.com/yugabyte/yugabyte-db
- YugabyteDB Kubernetes Operator: Apache-2.0, https://github.com/yugabyte/yugabyte-k8s-operator
- YugabyteDB Anywhere / Yugaware: separate installed software terms, https://www.yugabyte.com/legal/
- PostgreSQL: PostgreSQL License, https://www.postgresql.org/about/licence/
- NATS: Apache-2.0, https://github.com/nats-io/nats-server
- KubeVirt: Apache-2.0, https://github.com/kubevirt/kubevirt
- K8ssandra: Apache-2.0, https://github.com/k8ssandra/k8ssandra-operator
- Apache Cassandra: Apache-2.0, https://github.com/apache/cassandra
- Argo CD: Apache-2.0, https://github.com/argoproj/argo-cd
EOF

echo "Wrote ${OUT}"
