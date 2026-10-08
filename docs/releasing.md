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
bash scripts/verify-package.sh /tmp/cutline-v0.1.0 v0.1.0
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

## Exact candidate acceptance

Require green native, PostgreSQL, and Temporal jobs on the reviewed commit, with no skipped required gate. The native and Temporal gates exercise `run → minimize → saved capsule → replay → report`. Replay the same saved capsule twenty times with at least nineteen exact signatures; preserve artifacts and assert the report's contract, signature, schedule and evidence, not only its title. Verify changed targets and unsupported adapter/schema identities are rejected before execution; check failure exit codes in text and JSON modes.

Packaged CLI verification checks archive checksums, executable/BUILDINFO presence, clean exit 0, and faulty exit 2 with the named contract violation. Linux amd64 is executed in CI; arm64 is cross-built, not runtime-tested there. A release tag or published draft is a separate maintainer action after these gates.

The line budget counts non-generated Go source, including tests, comments and blank lines, excluding vendor and assets. Keep the local first release within 14,400 Go lines; do not meet this by deleting meaningful regression coverage. Documentation, SQL, YAML and shell scripts are tracked separately.
