# Releasing Cutline

Cutline is currently supported on Linux. Releases are built as statically linked
Linux archives for `amd64` and `arm64`; Cutline intentionally does not publish
a package for remote Temporal or remote worker operation.

## Preconditions

Run the complete verification set from a clean checkout. PostgreSQL and Temporal
checks require Docker.

```sh
make release
```

This runs format verification, vet, unit tests, race tests, the PostgreSQL
integration test, the local Temporal integration test, the reference replay
gate, and a production CLI build. Do not make a release from an unverified
local change or from a capsule containing production traces or credentials.

## Build release archives

Choose a semver tag and an empty output directory:

```sh
make package VERSION=v0.1.0 OUT_DIR=/tmp/cutline-v0.1.0
```

The command creates:

- `cutline_v0.1.0_linux_amd64.tar.gz`
- `cutline_v0.1.0_linux_arm64.tar.gz`
- `checksums.txt`

Each archive contains the binary, `README.md`, `LICENSE`, and `BUILDINFO`
with the source commit and Go toolchain version. The packaging script refuses
to overwrite an existing output directory.

## Publish checklist

1. Confirm the tag points to the reviewed commit and the CI workflow is green.
2. Verify the checksums from a fresh download.
3. Create a GitHub release marked as a draft, upload both archives and
   `checksums.txt`, and use the tag as the release title.
4. Keep the release draft until a maintainer has checked the quick-start fixture
   on a clean Linux environment.

Releases are not a claim that Cutline proves arbitrary concurrent programs
correct. The published scope remains bounded checkpoint schedules, local
Temporal history ingestion, and explicit evidence contracts.
