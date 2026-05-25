# Third-Party Notices

Servicer is licensed under the Apache License, Version 2.0. Servicer also
depends on third-party open source software and can install or render
manifests for external service operators. Those projects keep their own
licenses, copyrights, notices, trademarks, and support terms.

Distribution artifacts should include this file, the Servicer `LICENSE`, and a
generated third-party license bundle from:

```sh
./hack/generate-third-party-licenses.sh
```

The generated bundle is written to `dist/THIRD_PARTY_LICENSES/`.

## Runtime and Build Dependencies

Servicer Go binaries are built from modules listed in `go.mod` and `go.sum`.
The web UI is built from packages listed in `web/package.json` and
`web/package-lock.json`.

Before publishing source archives, binaries, container images, or appliance
bundles, regenerate `dist/THIRD_PARTY_LICENSES/` and include it beside the
artifact. This preserves upstream license texts where they are present in the
Go module cache and npm package tree.

Known license families in the current web dependency tree are permissive:
MIT, Apache-2.0, BSD-2-Clause, BSD-3-Clause, ISC, and Python-2.0.

## Operator and Service Software

Servicer references these upstream operators and services through catalog
metadata, demo scripts, or rendered Kubernetes manifests. They are not
relicensed by Servicer.

| Component | Current use | License / terms |
| --- | --- | --- |
| CloudNativePG | PostgreSQL operator package and CNPG runtime manifests | Apache-2.0 |
| External Secrets Operator | Optional credential delivery operator package | Apache-2.0 |
| Valkey | Valkey service class/runtime manifests | BSD-3-Clause |
| YugabyteDB core | YugabyteDB service class/runtime target | Apache-2.0 |
| YugabyteDB Kubernetes Operator | Yugabyte operator package/demo install | Apache-2.0 |
| YugabyteDB Anywhere / Yugaware | Demo operator chart component only | Separate Yugabyte installed software terms; do not bundle or redistribute as Apache-licensed Servicer software |
| PostgreSQL | Database engine managed through CloudNativePG | PostgreSQL License |
| NATS | NATS service class/runtime manifests | Apache-2.0 |
| KubeVirt | VM service class/runtime manifests | Apache-2.0 |
| K8ssandra / Apache Cassandra | Cassandra service class/runtime manifests | Apache-2.0 |
| Argo CD | Optional GitOps delivery integration | Apache-2.0 |

## Redistribution Rules

- Keep Servicer's `LICENSE` with all source and binary distributions.
- Keep upstream license texts and notice files for bundled dependencies.
- Do not represent third-party operators, charts, images, or databases as
  being owned by or licensed by Servicer.
- Keep product names and trademarks attributed to their upstream owners.
- Treat YugabyteDB Anywhere / Yugaware as separately licensed software. It may
  be referenced for demo installation, but must not be bundled into Servicer
  distribution artifacts unless its license terms have been reviewed and
  accepted for that distribution channel.

This notice is not legal advice. It is a repository-level compliance control so
release artifacts carry the license material required by Servicer's direct and
transitive dependencies.
