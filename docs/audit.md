# Audit

Servicer emits structured audit records for:

- mutating BFF requests and credential reveal/download requests
- auth provider, user, group, and role binding changes
- tenant, project, cluster, service class, repository, namespace claim, service instance, action, approval, and deletion requests
- credential detail and download access
- Kubernetes Events and ActionRequests that are merged into audit search results

The BFF attaches `X-Request-Id` and `X-Correlation-Id` to each response. If the client supplies `X-Request-Id`, it is echoed; otherwise the BFF generates one. Persisted BFF audit records include `requestId`, so operators can correlate the browser/API response, BFF logs, audit ConfigMap, and downstream controller annotations on created ServiceInstances, NamespaceClaims, and ActionRequests.

Credential reveal and credential download endpoints are audited as BFF requests because their path contains `/credentials/`. The audit entry records actor, route pattern, HTTP method, status-derived phase, request ID, and involved API path. The audit record does not include secret values.

Authentication failures increment BFF metrics and return structured errors, but they are not currently persisted as durable audit ConfigMaps unless they occur through an audited non-auth API path. Production operators should keep IdP logs and BFF JSON logs for login/logout evidence.

The default durable audit sink stores audit records as labeled ConfigMaps in `servicer-system` with `SERVICER_AUDIT_RETENTION_DAYS` retention. Production installs should also set `SERVICER_AUDIT_STDOUT=true` and forward JSON logs to the platform SIEM or log pipeline. Audit persistence failures are logged and counted by `servicer_bff_audit_persist_failures_total`.

Filtering is available on `/api/audit` with `q`, `type`, `actor`, `resource`, `action`, `phase`, `from`, `to`, and `limit`.

Tenant scoping is enforced when building audit results: non-platform-admin users only see events for visible tenants, projects, instances, and involved runtime objects. Users with the `auditor` role can access the audit endpoint without platform-admin mutation privileges.
