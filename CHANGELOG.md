# Changelog

All notable changes to this project are documented in this file.

## [0.1.0] - 2026-05-25

Initial production release.

### Added

- Tagged build pipeline for container image publication across components.
- Tagged release artifact publication for `servicer-install-<version>.yaml`.
- Release security workflow for image scan and keyless signing.
- Production-oriented docs for install, supply chain, compatibility, and operations.
- Feature gap tracking for preview and partial product areas.

### Notes

- API remains `platform.servicer.io/v1alpha1` with compatibility rules documented in `docs/api-compatibility.md`.
- See detailed notes in [docs/releases/v0.1.0.md](docs/releases/v0.1.0.md).
