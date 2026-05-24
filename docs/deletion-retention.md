# Deletion And Retention

Every production plan must define a deletion policy:

- `Delete`: remove generated runtime resources
- `Retain`: leave runtime resources for manual recovery
- `Snapshot`: create or require a backup/snapshot before destructive cleanup

Database, volume, and VM products default to snapshot-first deletion. Destructive actions require approval where the adapter marks the action disruptive. Failed deletions must preserve finalizers until cleanup can be retried or explicitly orphaned by a platform admin.

Accidental deletion recovery uses the control-plane backup/restore workflow plus product-specific backup media. GitOps delivery repositories remain the source of truth for generated manifests.
