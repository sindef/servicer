# Supply Chain

Release controls:

- Go dependency verification in CI
- `govulncheck` for Go vulnerability reachability
- `gosec` static security checks
- Helm CLI smoke test against the manager-pinned Helm version
- Trivy filesystem scan in validation
- Trivy image scans for every release image
- BuildKit SBOM and provenance generation for every image
- keyless cosign signing for tagged release images
- multi-architecture images for `linux/amd64` and `linux/arm64`
- checksum verification for downloaded Helm and kubectl binaries in Containerfiles

Production install manifests use release tags. Operators can pin image digests in a downstream kustomize overlay after release artifacts are published.

Local pre-push parity check:

- `./hack/ci-local-build-security.sh` runs validate checks plus local image
  build and Trivy vulnerability scans aligned with the CI build workflow.
