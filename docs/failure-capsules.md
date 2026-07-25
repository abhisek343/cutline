# Failure Capsules

## Goal

A failure capsule is the smallest portable artifact needed to understand and
replay one stable Cutline violation.

It is not a general backup of the test environment and must not contain secrets
or production data.

## Layout

```text
capsule/
  manifest.json
  campaign.yaml
  target.json
  dependencies.json
  schedule.json
  events.jsonl
  effects.json
  graph.json
  evaluation.json
  minimization.json
  replay/
    README.md
  report/
    index.html
    data.json
  checksums.sha256
```

## Manifest

The manifest includes:

- capsule schema version;
- Cutline version and build digest;
- capsule ID and creation time;
- campaign and target digests;
- adapter name, version, and capabilities;
- failure signature;
- deterministic seed;
- artifact list, media types, sizes, and SHA-256 digests;
- redaction policy and redaction count;
- replay prerequisites;
- stability confirmation result.

## Schedule

The minimized schedule records:

- checkpoint declaration and visit identities;
- ordered release decisions;
- cancellation trigger and target;
- deadline advances when used;
- scheduler precondition digests;
- termination and drain decisions.

It does not rely on wall-clock sleeps as a source of order.

## Evidence

`events.jsonl` is the canonical ordered event stream. `effects.json` and
`graph.json` are derived indexes included for portability and report rendering.
Their digests are validated, and replay evaluation can rebuild them from events.

## Target and dependencies

The capsule records immutable references when possible:

- Git commit and dirty-state flag;
- executable or container image digest;
- Go and module metadata;
- Temporal server and SDK versions;
- PostgreSQL schema version;
- fixture image digests;
- safe configuration digest.

It does not embed arbitrary binaries or database dumps by default.

## Replay

The intended command is:

```text
cutline replay path/to/capsule
```

Replay performs:

1. archive and path safety validation;
2. checksum verification;
3. schema compatibility check;
4. prerequisite and target digest check;
5. isolated dependency provisioning;
6. execution of the minimized schedule;
7. evidence freeze and contract evaluation;
8. failure-signature comparison;
9. replay result report.

An override may permit a different target digest for fix verification, but the
result is labelled a comparative replay rather than an exact reproduction.

## Minimization record

`minimization.json` includes:

- original and final schedule sizes;
- candidates evaluated;
- accepted and rejected reductions;
- run/time budget;
- confirmation policy;
- failure signatures observed;
- final stability score;
- termination reason.

This makes "minimal" an auditable claim.

## Integrity and safety

- archive entries cannot escape the destination directory;
- symlinks and device files are rejected;
- artifact sizes and total expanded size are capped;
- every listed file has a checksum;
- HTML treats capsule data as escaped data, not executable markup;
- secrets and configured fields are redacted before hashing and packaging;
- imports never execute replay automatically.

## Compatibility

Readers support declared capsule schema versions. Incompatible major versions
are rejected with a migration message. Migration creates a new capsule and
preserves the original.

## Retention

Capsules are test evidence. Teams should define retention separately from the
PostgreSQL run ledger. A capsule may be committed only when its contents are
small, deterministic, reviewed, and free of sensitive data.
