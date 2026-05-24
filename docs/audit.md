# Audit

Servicer emits structured audit records for:

- login, logout, and failed authentication
- auth provider, user, group, and role binding changes
- tenant, project, cluster, service class, repository, namespace claim, service instance, action, approval, and deletion requests
- credential detail and download access
- Kubernetes Events and ActionRequests that are merged into audit search results

The default durable audit sink stores audit records as labeled ConfigMaps in `servicer-system` with `SERVICER_AUDIT_RETENTION_DAYS` retention. Production installs should also set `SERVICER_AUDIT_STDOUT=true` and forward JSON logs to the platform SIEM or log pipeline.

Filtering is available on `/api/audit` with `q`, `type`, `actor`, `resource`, `action`, `phase`, `from`, `to`, and `limit`.

Tenant scoping is enforced when building audit results: non-platform-admin users only see events for visible tenants, projects, instances, and involved runtime objects. Users with the `auditor` role can access the audit endpoint without platform-admin mutation privileges.
