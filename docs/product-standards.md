# Product Standards

Servicer product adapters expose a stable product contract even when the backing runtime differs by operator or cluster.

## Relational database naming

Applies to:

- PostgreSQL (`postgresql`)
- MySQL (`mysql`)
- YugabyteDB (`yugabyte`) when used for YSQL / relational access

### Default database name

If the requester does not set `parameters.databaseName`, Servicer derives the initial database name from the `ServiceInstance` name.

Examples:

- `orders-db` -> `orders_db`
- `billing` -> `billing`
- `123-demo` -> `db_123_demo`

### Normalization rules

Servicer normalizes relational database names to keep the product contract portable across adapters:

- lowercase only
- letters and digits are preserved
- hyphens, spaces, and underscores collapse to `_`
- unsupported characters are removed
- names starting with a digit are prefixed with `db_`
- names are capped at 63 characters

### Request parameter

Relational products accept:

- `parameters.databaseName`

If set, Servicer uses the provided value after applying the same normalization rules above.

### Adapter expectations

- PostgreSQL adapters should bootstrap the initial application database using the resolved database name.
- YugabyteDB adapters should publish the resolved database name in the connection credential contract and use it for bootstrap workflows as runtime support matures.

### Credential expectations

When Servicer publishes or projects relational credentials, the credential payload should expose the resolved database name via a `database` field when the runtime does not already provide an equivalent field.

## MySQL Product Standard

Servicer exposes MySQL as an operator-neutral relational product. The public contract is centered on database intent rather than any one operator's CRD.

### Supported topology models

- `single-cluster`
  Servicer-owned StatefulSet with one primary service surface and optional same-cluster replicas.
- `multi-region` with `parameters.replicationMode: galera`
  Multi-cluster peer topology intended for quorum-based synchronous writes.
- `multi-region` with `parameters.replicationMode: active-passive`
  One writable primary cluster and one or more standby clusters. Failover is initiated through Servicer actions and updates the desired primary/standby placement.

### Common parameters

- `parameters.databaseName`
- `parameters.replicas`
- `parameters.cpu`
- `parameters.memory`
- `parameters.storageClass`
- `parameters.storageSize`
- `parameters.backupProfile`
- `parameters.primaryCluster`
- `parameters.standbyClusters`
- `parameters.replicationMode`

### Runtime and credential expectations

- If `parameters.databaseName` is omitted, the initial database name derives from the instance name using the shared relational naming rules.
- The published application credential secret should include `username`, `password`, and `database`.
- `single-cluster` MySQL defaults to `replicationMode: single-primary`.
- `multi-region` MySQL plans require at least one standby or peer cluster in `parameters.standbyClusters`.
- Active-passive failover changes Servicer's desired primary cluster and relies on normal reconciliation to materialize the updated topology.

## NATS messaging contract

Applies to:

- NATS (`nats`)

### Managed resource model

Servicer treats a NATS instance as more than broker replicas. The product contract may also declare:

- `parameters.streams`
- `parameters.consumers`
- `parameters.appCredentials`

These are part of the product API, not a direct leak of any operator CRD.

### Streams

Each stream should declare:

- `name`
- `subjects`

Optional fields include:

- `storage`
- `retention`
- `maxAge`
- `maxMsgs`
- `maxBytes`
- `replicas`

JetStream must be enabled when streams are declared.

### Consumers

Each consumer should declare:

- `name`
- `stream`

Optional fields include:

- `filterSubjects`
- `ackPolicy`
- `deliverPolicy`
- `replayPolicy`
- `maxAckPending`

Consumers are reconciled against declared streams and must reference a stream in the same product request.

### App credentials and permissions

Each app credential may declare:

- `name`
- `username`
- `permissions.publish`
- `permissions.subscribe`
- `permissions.allowResponses`

Servicer generates one Secret per app credential and one server auth-config Secret used by the NATS runtime.

### Lifecycle

- create/update of streams and consumers is controller-managed from desired parameters
- targeted deletes and purges are exposed as actions
- credential rotation can target the admin credential or an individual app credential
