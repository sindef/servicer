# Production Install

Servicer production installs use the `deploy/` kustomization. The install assumes:

- images are pinned to a release tag, then optionally pinned by digest in your environment overlay
- TLS terminates at ingress and forwards traffic to the `web` Service
- `SERVICER_AUTH_EXTERNAL_BASE_URL` is set to the canonical external HTTPS URL users see in their browser
- generated delivery artifacts are committed and pushed to a GitOps repository
- Argo CD syncs the committed artifacts to target clusters
- the local development syncer sidecar is not part of the production path

Before applying `deploy/`, replace these values:

- `deploy/ingress.yaml`: set the production host, `ingressClassName`, TLS Secret, and certificate issuer
- `deploy/bff.yaml`: replace `https://servicer.example.com` with the production external URL
- create `servicer-bff-session` with a randomly generated signing secret
- create or update `servicer-delivery-repo` with the Git repository URL, known hosts, and deploy key
- configure login throttling settings and any compensating edge controls

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

## External URL and Proxy Headers

`SERVICER_AUTH_EXTERNAL_BASE_URL` must be the stable HTTPS origin for the BFF/web UI. OIDC redirect URLs and logout redirects are generated from this value when it is set. If it is wrong, browser login callbacks can fail or point at the wrong host.

`SERVICER_TRUSTED_PROXY_HEADERS=true` makes the BFF trust sanitized ingress headers for request scheme and client IP. Only enable it when the ingress strips untrusted client-supplied `X-Forwarded-*` headers and injects its own values. Otherwise leave it disabled and the BFF falls back to `RemoteAddr`.

## GitOps Delivery Requirements

Production-mode ServiceInstances that render delivery artifacts require the delivery repository publisher to be enabled with auto-commit and auto-push. A successful API create/update means the CRD request was accepted; provisioning does not appear ready unless the controller can publish or push delivery artifacts and reconcile Argo CD state. Delivery failures are surfaced in `ServiceInstance.status`, controller metrics, and Prometheus rules.

Repository credentials registered through the BFF are mirrored into Argo CD repository Secrets. Create and delete operations report failure when the Argo mirror cannot be written or removed instead of silently treating the API operation as successful.

## Login Rate Limiting

The currently implemented limiter backend is `SERVICER_LOGIN_RATE_LIMIT_BACKEND=memory`. It enforces `5` failed login attempts per provider/user/client-IP key in a 15-minute window and locks that key for 15 minutes.

This backend is per BFF pod. In production mode (`SERVICER_PRODUCTION=true`), startup fails unless `SERVICER_LOGIN_RATE_LIMIT_ACCEPT_IN_MEMORY=true` is explicitly set. The base production manifest sets that acknowledgement because no shared limiter backend exists yet; operators should use an IdP policy, WAF/ingress rate limit, or single-replica BFF deployment if stronger global throttling is required.

## Auth Identity Linking

External OIDC and LDAP identities must be linked to a Servicer `User` through `spec.externalIdentities`. OIDC subjects are matched against the issuer-qualified subject and the raw `sub` claim for legacy compatibility. LDAP subjects use the configured username attribute, falling back to the user DN.

Role grants come from `RoleBinding` subjects that match linked users or local/external groups. Tenant owner fields remain a legacy visibility path and should reference Servicer user names or mapped group names, not arbitrary IdP display names. Local password login requires `spec.localAuth.enabled=true` and a password hash Secret reference.

Production network policy defaults deny all traffic in `servicer-system`, then allow:

- an ingress controller in namespace `ingress-nginx` to `web`
- `web` to `bff`
- Prometheus in namespace `monitoring` to BFF and manager metrics
- manager webhook traffic from any namespace to port `9443`
- DNS egress to `kube-system`
- BFF broad TCP/443 egress for Kubernetes API, OIDC, and LDAPS endpoints
- manager HTTPS/SSH egress for Argo CD and Git

If your ingress, monitoring, DNS, Argo CD, Git, LDAP, or OIDC endpoints live elsewhere or need non-443 ports such as LDAP StartTLS on 389, adjust `deploy/network-policies.yaml` in an overlay before rollout.

## Component Permissions

`servicer-manager` owns reconciliation. It needs explicit access to Servicer CRDs, runtime namespaces, generated credentials, workload observation, External Secrets resources, Argo CD Applications, webhook certificates, and leader-election Leases.

`servicer-bff` owns browser/API requests. It can read and mutate Servicer CRDs through authorized endpoints, read runtime state for detail pages, manage repository credential Secrets, and persist audit ConfigMaps.

`servicer-webhook-bootstrap` only creates/updates the webhook serving Secret and patches admission webhook configurations.

`servicer-control-plane-backup` only reads Servicer CRDs and selected runtime state for backup/restore jobs.

Wildcard RBAC rules are not allowed in production manifests.

## Demo-Only Inputs

Do not copy `config/deploy` or `config/samples` credentials into production. The demo path includes local images, local kubeconfigs, `demo-admin` login data, and operator bootstrap constants intended only for Kind/demo validation. Production users, providers, cluster targets, and operator defaults must be created from environment-specific Secrets and reviewed catalog data.
