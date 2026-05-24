# Tenant Threat Model

Roles:

- platform admin: full control-plane administration
- tenant admin: tenant-scoped ownership and repository governance
- tenant operator: tenant/project operations without platform mutation
- catalog admin: catalog and package management
- cluster admin: cluster target management
- service consumer: project-scoped service requests
- auditor: read-only audit visibility

Isolation boundaries:

- Kubernetes namespaces created from project namespace strategy
- Argo CD Applications and Projects owned by a tenant/project
- repository credentials stored per tenant or project
- service credentials exposed only through visible ServiceInstances and bindings
- audit events filtered through visible tenants/projects/instances for non-platform-admin actors

Negative authorization coverage lives in BFF tests for cross-tenant service, namespace, credential, and Kubernetes proxy access. New BFF endpoints must add a negative authorization test before merge.
