# Scale Limits

First production target envelope:

| Object | Target |
| --- | ---: |
| Tenants | 100 |
| Projects | 1,000 |
| ServiceInstances | 10,000 |
| NamespaceClaims | 5,000 |
| Repositories | 2,000 |
| ActionRequests retained | 100,000 |
| Audit records retained | 1,000,000 or external sink limit |

BFF list endpoints should use server-side filtering and pagination before exceeding these targets. Current high-volume endpoints with server-side `limit`/`offset`/`q` support:

- `GET /api/tenants`, `GET /api/projects`, `GET /api/instances`
- `GET /api/projects/{name}/repositories`, `GET /api/tenants/{name}/repositories`

Query parameters:

- `limit` (default `100`, max `500`)
- `offset` (default `0`)
- `q` (case-insensitive text filter on name/display and other key summary fields)

These endpoints still return the existing array response shape; there is no total-count or continuation-token envelope yet. `/api/audit` supports filtering and `limit` with a max of `500`, but it still builds the visible event set before applying the limit. Other lower-volume list endpoints should be reviewed before object counts approach the target envelope.

Controller scale testing must watch Kubernetes API QPS, controller cache memory, Argo CD API behavior, and audit store growth.

Load tests should exercise overview, list/detail, repository, audit, and Kubernetes proxy endpoints at the object counts above before each GA release candidate.
