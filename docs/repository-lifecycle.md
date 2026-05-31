# Repository Lifecycle

Repositories are scoped to tenant or project endpoints:

- `/api/tenants/{tenant}/repositories`
- `/api/projects/{project}/repositories`

Credentials are stored in Kubernetes Secrets labeled by scope. Repository create and delete requests are audited as mutating BFF requests. Registration also mirrors the credential into an Argo CD repository Secret. The API only reports success after that mirror is created or updated successfully; mirror failures return an error and the repository registration is rolled back.

Rotation is performed by creating a replacement credential, updating active services to the new repository reference, then deleting the old repository Secret after dependent services report synced.

Deletion rule: a repository cannot be removed while active ServiceInstances or Managed Applications reference it by `repoRef` or `repoURL` in their parameters. The check is enforced for project-scoped repositories within that project, and for tenant-scoped repositories across all projects in the tenant. The API returns `409 repository_in_use` with the blocking dependencies. Operators must migrate or delete those services first. When deletion succeeds, the matching Argo CD repository Secret mirror is removed unless another registered repository still maps to the same canonical URL and scope.

Argo CD repository Secret names include a hash suffix over the canonical repository URL and repository scope, so long URLs that share the same prefix cannot collide by truncation. Private repository registration is covered by the GitOps e2e workflow.

Repository URLs are canonicalized and validated before storage. Public/no-auth repositories must use `https://`; HTTP credential auth must also use `https://`; SSH auth must use `ssh://` or `git@host:path` syntax and include an SSH private key. Embedded credentials in URLs are rejected.
