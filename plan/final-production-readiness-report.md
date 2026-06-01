# Final Production Readiness Report

Date: 2026-06-01  
Scope: `/home/mnorris/repos/servicer`

## Recommendation

**Do not ship**

## Re-read and Audit Reconciliation

Reviewed:

- `plan/prelaunch-audit.md`
- `plan/do-not-ship.md`
- files changed by hardening commits: `4228ef3`, `cf887bf`, `bc89924`, `7b71146`, `6c3f99e`, `a6782d9`, `0432841`, `36872ea`, `31be819`, `eab0898`, `1484e5a`, `d898ffa`, `a64aa2f`, `36ef9ea`

## Original Launch Blockers (from prior do-not-ship list) and Resolution

1. OIDC mutable identity binding / implicit linking  
Status: **Resolved**  
Evidence: `4228ef3`, `internal/bff/authenticator.go`, tests in `internal/bff/authenticator_test.go`.

2. ActionRequest side effects not idempotent  
Status: **Resolved**  
Evidence: `1484e5a`, `internal/controllers/actionrequest_controller.go`, `internal/controllers/actionrequest_idempotency_test.go`.

3. BFF RBAC too broad  
Status: **Resolved (material reduction)**  
Evidence: `d898ffa`, `deploy/rbac.yaml`, `config/deploy/rbac.yaml`, `hack/rbac-policy.sh`.

4. NetworkPolicy web->BFF port mismatch  
Status: **Resolved**  
Evidence: `a64aa2f`, `deploy/network-policies.yaml`, `hack/networkpolicy-smoke.sh`.

5. Namespace kubeconfigs insecure TLS  
Status: **Resolved**  
Evidence: `eab0898`, `internal/controllers/actionrequest_controller.go`, tests in `internal/controllers/actionrequest_controller_test.go`.

6. Argo repo mirror errors swallowed  
Status: **Resolved**  
Evidence: `31be819`, `internal/bff/repositories.go`, `TestCreateProjectRepositoryRollsBackWhenArgoMirrorFails`.

7. Repository delete lacks active-reference checks  
Status: **Resolved**  
Evidence: `31be819`, `internal/bff/repositories.go`, `TestDeleteProjectRepositoryRejectsActiveReferences`.

8. ClusterTarget client cache not invalidated on Secret rotation  
Status: **Resolved**  
Evidence: `31be819`, `internal/controllers/serviceinstance_controller.go`, `TestGetTargetClientRefreshesAfterSecretRotationAndTargetUpdate`.

9. Frontend auth bootstrap blank/stuck risk  
Status: **Resolved**  
Evidence: `6c3f99e`, `web/src/auth.ts`, `web/src/main.ts`, `web/src/auth.test.ts`.

10. PR/release automated gates missing  
Status: **Resolved**  
Evidence: `0432841`, `.github/workflows/build.yml`, `.github/workflows/e2e-gitops.yml`.

## Current Do-Not-Ship Items and Verification

### Finding 9: Namespace proxy scope still too broad

- Current code still uses path heuristics:
  - `internal/bff/kubernetes_proxy.go:149-157`
  - `/apis/` requests are allowed unless mismatched namespace appears in path.
- Verification command:
  - `go test ./internal/bff -run 'TestNamespaceProxyForwardsGrantedReadOnlyRequest|TestKubernetesRootProxySupportsKubectlDiscovery|TestKubernetesRootProxyRejectsDifferentNamespace'`
  - Result: `ok  	github.com/sindef/servicer/internal/bff	0.076s`
- Conclusion: tests pass, but they do **not** enforce explicit namespace-scoped resource allowlisting. Blocker remains open.

### Finding 47: Unresolved third-party license metadata

- File present: `dist/THIRD_PARTY_LICENSES/web/MISSING_LICENSE_FILES.tsv`
- Verification command:
  - `./hack/generate-third-party-licenses.sh`
  - Result: `Unapproved missing license files detected: 3` (exit 1)
- Conclusion: compliance blocker remains open.

## Full Validation Commands and Results

1. `go test ./...`  
Result: **pass**

2. `cd web && npm run lint && npm run test:unit && npm run test:a11y && npm run build && npm audit --omit=dev`  
Result: **pass** (`3 passed` a11y tests, build completed, `found 0 vulnerabilities`)

3. `kubectl apply --dry-run=server -f deploy/ || true`  
Result: **non-fatal errors present** (as expected with `|| true`): missing CRD mappings in current cluster and immutable-field webhook rejections against already-existing resources in this environment.

4. `go test -race -coverprofile=coverage.out ./...`  
Result: **fail** (`go: no such tool "covdata"` for `cmd/bff` and `cmd/manager` in this environment/toolchain path)

5. `go test ./api/v1alpha1 -run 'Webhook|Validate'`  
Result: **pass**

6. `go test ./internal/controllers -run 'Reconciler|Controller'`  
Result: **pass**

7. `go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...`  
Result: **fail** due local Go toolchain mismatch (`application built with go1.23` while modules require newer Go versions, including go1.26)

8. `go run github.com/securego/gosec/v2/cmd/gosec@v2.22.5 ./...`  
Result: **fail** with 1 finding:
   - `internal/controllers/clustertarget_controller.go:488` `G304` Potential file inclusion via variable.

9. `kubectl kustomize config/deploy > /dev/null`  
Result: **pass**

10. `kubectl kustomize deploy > /dev/null`  
Result: **pass**

11. `./hack/manifest-policy.sh deploy`  
Result: **pass**

12. `./hack/rbac-policy.sh deploy`  
Result: **pass**

13. `./hack/networkpolicy-smoke.sh`  
Result: **pass** (`NetworkPolicy manifest smoke passed (4 policies rendered).`)

14. `./hack/helm-cli-smoke.sh`  
Result: **pass** (`Helm smoke check passed for v4.2.0`)

## KinD E2E

Command:

- `./hack/e2e-gitops.sh`

Result: **fail**  
Failure point: `kubectl wait -n argocd --for=condition=Available deploy/argocd-repo-server --timeout=180s` timed out.

Observed cluster state during failure:

- `argocd-repo-server` `CreateContainerConfigError`
- events showed repeated `Error: secret "argocd-redis" not found`

## Remaining Risks (Explicit)

- **Blocking**: Finding 9 namespace proxy scope is not explicit allowlist enforcement.
- **Blocking**: Finding 47 unresolved license metadata exceptions.
- **High operational risk**: KinD GitOps e2e did not pass in this run.
- **Security/tooling gate gap in this environment**: `govulncheck` and `gosec` did not complete cleanly (`gosec` surfaced one issue; `govulncheck` toolchain mismatch).

## Ship Decision

**Do not ship** until:

1. Finding 9 and finding 47 are closed with tests and docs.
2. GitOps KinD e2e passes on a clean cluster.
3. Security scan gate is green (or approved exception process is documented and signed off).
