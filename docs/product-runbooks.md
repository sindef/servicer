# Product Runbooks

Production drills:

- Namespace: quota, default-deny network policy, deletion, and cross-tenant authorization
- PostgreSQL/CNPG: backup, restore, failover, credential rotation, and operator upgrade
- YugabyteDB: xCluster setup, backup, restore, failover, and lag telemetry
- Argo application: private repo credentials, per-tenant scoping, failed push, and delivery drift

Preview drills before promotion:

- MySQL: backup/restore execution and topology failover
- Valkey: failover, health, credential rotation, and standby resync
- NATS: JetStream, gateway health, replication lag, and recovery
- Cassandra/K8ssandra: repair, backup, health, and action execution
- KubeVirt/VM: guest readiness, DataVolume status, power actions, console access, and backup policy

No preview product should be added to the production support set until its drill is automated or explicitly accepted as a release risk.
