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

Auth identity behavior:

- OIDC and LDAP users must resolve to a Servicer `User` through explicit external identity linkage before they receive Servicer roles.
- OIDC matching accepts the issuer-qualified subject and the raw `sub` claim for legacy compatibility.
- Local users authenticate by Servicer user name and `spec.localAuth`; disabling local auth leaves external identities as the login path.
- RoleBindings may target linked users or Servicer groups. External IdP groups only grant roles after being mapped to a Servicer `Group`.
- Tenant owner fields are still honored for compatibility and can make a tenant visible when they match the actor's linked user name, email, subject, or group.

Namespace proxy behavior:

- Namespace kubeconfig downloads are only produced by completed namespace `grant-access` ActionRequests.
- The generated kubeconfig points back through the BFF namespace proxy, not directly at the target cluster.
- Generated namespace kubeconfigs do not use `insecure-skip-tls-verify`; they rely on public TLS trust or include `certificate-authority-data` when `SERVICER_NAMESPACE_ACCESS_CA_DATA` is configured on the manager.
- The proxy accepts bearer tokens from the grant Secret and allows read-only namespace API discovery/read paths for the granted namespace.
- Requests for other namespaces, write methods, or non-namespace paths are denied and counted by `servicer_bff_namespace_proxy_denials_total`.

Negative authorization coverage lives in BFF tests for cross-tenant service, namespace, credential, and Kubernetes proxy access. New BFF endpoints must add a negative authorization test before merge.
