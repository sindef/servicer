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

BFF list endpoints should add server-side filtering or pagination before exceeding these targets. Controller scale testing must watch Kubernetes API QPS, controller cache memory, Argo CD API behavior, and audit store growth.

Load tests should exercise overview, list/detail, repository, audit, and Kubernetes proxy endpoints at the object counts above before each GA release candidate.
