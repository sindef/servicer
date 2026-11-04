# Supply Chain

Release controls:

- Go dependency verification in CI
- builder stages pin the Go toolchain (`GOTOOLCHAIN`) so image builds honor the
  `go` directive in `go.mod` even when the digest-pinned builder image ships an
  older toolchain
- `govulncheck` for Go vulnerability reachability
- `gosec` static security checks
- Helm CLI smoke test against the manager-pinned Helm version
- Trivy filesystem scan in validation
- registry-free third-party license bundle check in every pull request
  (`hack/check-license-bundle.py`, run by the `Build` workflow's `Validate`
  job): bundle self-consistency, plus every `go.mod` module version and every
  direct `web/package.json` dependency version resolved by
  `web/package-lock.json` must have a bundle entry
- third-party license bundle regeneration and drift check (`Release hygiene`
  workflow: weekly, on demand, and on pull requests that touch the dependency
  manifests, the generator, the offline gate or the bundle; regeneration needs
  access to the Go module graph and the npm registry). The `Validate` job re-runs
  the drift check for `v*` tags, so a stale bundle blocks the release gate
- Trivy image scans for every release image
- BuildKit SBOM and provenance generation for every image
- keyless cosign signing for tagged release images
- multi-architecture images for `linux/amd64` and `linux/arm64`
- checksum verification for downloaded Helm and kubectl binaries in Containerfiles

Production install manifests use release tags. Operators can pin image digests in a downstream kustomize overlay after release artifacts are published.

Local pre-push parity check:

- `./hack/ci-local-build-security.sh` runs validate checks plus local image
  build and Trivy vulnerability scans aligned with the CI build workflow.
