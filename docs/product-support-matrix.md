# Product Support Matrix

| Product | Production | Required package | Actions | Health semantics |
| --- | --- | --- | --- | --- |
| Namespace | Yes | None | `update-quota`, `grant-access` | ready, progressing, failed |
| PostgreSQL | Yes | `cnpg` | `backup` | ready, degraded, failed, backup stale, restore required |
| YugabyteDB | Yes | `yugabyte` | None until operator action drills complete | ready, degraded, failed, replication lag, restore required |
| Argo application | Yes | Argo CD preinstalled | None | synced, out of sync, degraded, failed |
| MySQL | Preview | None seeded | Hidden from production catalog | ready/progressing only until restore/failover matures |
| Valkey | Preview | None seeded | Hidden from production catalog | ready/degraded/failover semantics require operator drills |
| NATS | Preview | None seeded | Hidden from production catalog | gateway and JetStream recovery require operator drills |
| Cassandra/K8ssandra | Preview | None seeded | Hidden from production catalog | ring repair, backup, and health require operator drills |
| KubeVirt/VM | Preview | None seeded | Hidden from production catalog | guest readiness, storage, and power actions require operator drills |

Preview products can be enabled in non-production overlays and samples. Production catalog changes must update this matrix, `deploy/operator-packages.yaml`, and e2e coverage in the same pull request.
