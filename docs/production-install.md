# Production Install

Servicer production installs use the `deploy/` kustomization. The install assumes:

- images are pinned to a release tag, then optionally pinned by digest in your environment overlay
- TLS terminates at ingress and forwards traffic to the `web` Service
- generated delivery artifacts are committed to a GitOps repository
- Argo CD syncs the committed artifacts to target clusters
- the local development syncer sidecar is not part of the production path

Before applying `deploy/`, replace these values:

- `deploy/ingress.yaml`: set the production host, `ingressClassName`, TLS Secret, and certificate issuer
- create `servicer-bff-session` with a randomly generated signing secret
- create or update `servicer-delivery-repo` with the Git repository URL, known hosts, and deploy key
- configure login throttling backend settings (`SERVICER_LOGIN_RATE_LIMIT_BACKEND` and `SERVICER_LOGIN_RATE_LIMIT_ACCEPT_IN_MEMORY`)

Example session secret:

```bash
kubectl -n servicer-system create secret generic servicer-bff-session \
  --from-literal=session-secret="$(openssl rand -base64 48)"
```

Delivery repo secret:

```bash
kubectl -n servicer-system create secret generic servicer-delivery-repo \
  --from-literal=url="$SERVICER_DELIVERY_REPO_URL" \
  --from-file=ssh-privatekey="$HOME/.ssh/servicer_delivery" \
  --from-file=known_hosts="$HOME/.ssh/known_hosts" \
  --dry-run=client -o yaml | kubectl apply -f -
```

Restart the manager after changing this Secret so the repository URL environment variable refreshes. Production installs run the manager with `SERVICER_PRODUCTION=true`; when the Secret is absent, publishing is not auto-committing, or publishing is not auto-pushing, ServiceInstances that render delivery artifacts are marked `Degraded` with `DeliveryRepoRequired` and are not materialized locally as if delivery succeeded.

The manager clones the configured delivery repository into an ephemeral worktree, commits rendered artifacts with the configured commit identity, and pushes to the configured branch. If the remote is unavailable, the worktree is divergent, or push fails, reconciliation fails and records the Git error in ServiceInstance status instead of silently falling back to local-only delivery.

`SERVICER_LOGIN_RATE_LIMIT_BACKEND=memory` is a per-pod development fallback. In production mode (`SERVICER_PRODUCTION=true`), startup fails unless `SERVICER_LOGIN_RATE_LIMIT_ACCEPT_IN_MEMORY=true` is explicitly set to acknowledge the weaker multi-replica behavior. For ingress deployments, only enable trusted proxy mode when your ingress sanitizes and controls `X-Forwarded-For`; otherwise client IP identity falls back to `RemoteAddr`.

Production network policy defaults deny all traffic in `servicer-system`, then allow:

- ingress controller to `web`
- `web` to `bff`
- Prometheus to BFF and manager metrics
- Kubernetes API, Argo CD, DNS, and Git egress needed by BFF and manager

## Component Permissions

`servicer-manager` owns reconciliation. It needs explicit access to Servicer CRDs, runtime namespaces, generated credentials, workload observation, External Secrets resources, Argo CD Applications, webhook certificates, and leader-election Leases.

`servicer-bff` owns browser/API requests. It can read and mutate Servicer CRDs through authorized endpoints, read runtime state for detail pages, manage repository credential Secrets, and persist audit ConfigMaps.

`servicer-webhook-bootstrap` only creates/updates the webhook serving Secret and patches admission webhook configurations.

`servicer-control-plane-backup` only reads Servicer CRDs and selected runtime state for backup/restore jobs.

Wildcard RBAC rules are not allowed in production manifests.
