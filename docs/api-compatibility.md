# API Compatibility

The first production release keeps `platform.servicer.io/v1alpha1` as the served and storage API version. Servicer treats production `v1alpha1` as compatibility-managed even though the Kubernetes version suffix remains alpha.

Compatibility rules:

- additive fields are allowed in minor releases
- field removals require at least two minor releases of deprecation
- enum value removals require at least two minor releases of deprecation
- default changes that alter existing object behavior require release notes and an upgrade test
- breaking schema changes require a new served version and conversion webhook
- product contracts and plan names follow the same deprecation window as fields

Before introducing `v1beta1`, Servicer must add:

- conversion webhook implementation
- stored-object fixture coverage for every production CRD kind
- upgrade tests from the previous production release
- release notes documenting migrated/defaulted fields

Stored-object compatibility fixtures live in `api/v1alpha1/fixtures/stored-objects.yaml` and are validated by `go test ./api/v1alpha1`.
