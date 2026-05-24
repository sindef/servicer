# Repository Lifecycle

Repositories are scoped to tenant or project endpoints:

- `/api/tenants/{tenant}/repositories`
- `/api/projects/{project}/repositories`

Credentials are stored in Kubernetes Secrets labeled by scope and audited on create/delete/access. Rotation is performed by creating a replacement credential, updating active services to the new repository reference, then deleting the old repository Secret after dependent services report synced.

Deletion rule: a repository must not be removed while active services reference it. Operators should query instances for matching repository references and migrate or delete those services first. Private repository registration is covered by the GitOps e2e workflow.
