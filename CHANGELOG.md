# Changelog

All notable changes to Cutline will be documented here.

The project intends to follow Semantic Versioning after the first tagged
pre-release.

## Unreleased

### Fixed

- CEL temporal helpers now compare supplied boundaries with recorded transition timestamps and fail closed on malformed or missing temporal evidence.
- Temporal activity history correlation now follows scheduled event IDs, keeping retries and same-named concurrent activities distinct.
- Invalid cancellation observations are rejected from authoritative evidence, and the control handshake now terminates promptly with caller context cancellation.
- The pull-request demo workflow is read-only and uploads generated assets for verification without publishing or modifying repository contents.
- PostgreSQL sequence-gap and event-limit append failures now leave durable incomplete-evidence markers.

### Added

- Initial product, system-design, contract, testing, roadmap, and architecture
  decision baseline.
