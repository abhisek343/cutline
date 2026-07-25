# Testing Strategy

## Objectives

Tests must establish that Cutline:

- controls declared checkpoints predictably;
- distinguishes cancellation lifecycle boundaries;
- never turns missing evidence into a pass;
- detects seeded business-effect violations;
- preserves failure identity during minimization;
- replays capsules stably;
- keeps native Go and Temporal semantics aligned.

## Test layers

### Unit tests

Cover pure packages and state machines:

- campaign parsing, normalization, and validation;
- event envelope validation;
- task, cancellation, effect, and resource transitions;
- canonical sequence and causal-edge construction;
- schedule generation and bounds;
- CEL environment and helper functions;
- failure signature normalization;
- minimization candidate generation;
- capsule manifest and checksum validation;
- redaction and report escaping.

Use table-driven tests with explicit invalid cases.

### Property and model tests

Use generated event sequences to verify:

- invalid state transitions are rejected;
- frozen evidence is immutable;
- schedule actions reference reachable scheduler states;
- minimization never accepts a different failure signature;
- serialization round trips preserve semantics;
- reordering independent events does not change invariant results;
- duplicate delivery is idempotent where declared;
- incomplete evidence never evaluates to pass.

State-machine reference models should remain smaller than the implementation.

### Race and concurrency tests

Run `go test -race ./...` in CI. Target:

- scheduler ownership;
- SDK connection and acknowledgement handling;
- simultaneous point arrivals;
- cancellation/release races;
- target shutdown while evidence is in flight;
- concurrent effect transitions;
- cleanup idempotency.

A race-detector pass complements but does not replace Cutline's semantic tests.

### Integration tests

Use Testcontainers for:

- PostgreSQL migrations, constraints, transaction failure, and reconnection;
- local Temporal server and worker behavior;
- fixture dependencies with authoritative effect ledgers;
- coordinator/SDK Unix-socket protocol;
- capsule creation and exact replay;
- static report generation.

Pin image versions. Tests must time out and clean up by run ID.

### End-to-end benchmark fixtures

Each fixture has faulty and corrected variants:

1. **Checkout charge** — payment commits after cancellation observation.
2. **Duplicate notification** — timeout/cancel ambiguity causes two sends.
3. **Orphan worker** — child goroutine writes after parent return.
4. **Leaked reservation** — inventory reservation is not released.
5. **Temporal activity** — activity effect commits around cancellation heartbeat.

The bug must be understandable, intentional, and independently asserted by the
fixture dependency.

## Fixture contract

Every fixture provides:

- deterministic reset;
- a unique test namespace;
- authoritative effect query;
- known cancellation points;
- expected faulty failure signature;
- corrected expected pass;
- no production credentials or endpoints;
- bounded runtime.

## Required scenario matrix

| Scenario | Native Go | Temporal |
|---|---:|---:|
| Cancel before first effect | Yes | Yes |
| Cancel at checkpoint | Yes | Yes |
| Cancel between attempt and commit | Yes | Yes |
| Commit before observation | Yes | Yes |
| Commit after observation | Yes | Yes |
| Lost acknowledgement and retry | Yes | Yes |
| Repeated cancellation | Yes | Yes |
| Child outlives parent | Yes | Yes |
| Drain timeout | Yes | Yes |
| Control disconnect | Yes | Yes |
| Evidence sequence gap | Yes | Yes |
| Exact capsule replay | Yes | Yes |

## Oracle tests

For each contract:

- smallest passing evidence;
- smallest violating evidence;
- missing required capability;
- unknown effect outcome;
- duplicated and contradictory event;
- boundary equality;
- unrelated concurrent effect;
- compensated effect;
- schema-version mismatch;
- evaluation cost limit.

Golden files may validate exported JSON schemas, but behavioral assertions should
not rely only on large snapshots.

## Minimizer tests

Seed schedules with known irrelevant actions. Verify:

- the result preserves the same failure signature;
- essential cancellation and offending effect remain;
- schedule order remains valid;
- accepted reductions are reproducible;
- flaky candidates are rejected;
- budget exhaustion returns the best stable candidate;
- reduction history is auditable.

## Replay stability

Reference release gate:

- 20 exact replay attempts;
- at least 19 reproduce the same signature;
- no attempt produces contradictory evidence;
- environment and target digests match;
- run duration variance is reported but not used as identity.

## Negative tests

Cutline must reject:

- production-looking endpoints without override;
- malformed campaign or unsupported major version;
- duplicate checkpoint declarations with conflicting metadata;
- invalid CEL or excessive evaluation cost;
- path-traversal capsule;
- checksum mismatch;
- unescaped report payload;
- event or capsule size limit breach;
- shell-injection-style command arguments.

## Performance tests

Track, but do not prematurely optimize:

- SDK checkpoint round-trip latency;
- event ingest throughput;
- PostgreSQL batch latency;
- memory at 100,000 events;
- CEL evaluation duration;
- report generation duration;
- minimization attempts per minute.

Performance regression thresholds should be based on benchmark history after the
first vertical slice.

## CI gates

Planned pull-request gates:

```text
format
vet
unit
race
integration-native
schema-compatibility
security/static-analysis
```

Temporal integration and full benchmark replay may run on `main` or nightly until
CI duration is measured.

## Test reporting

Every implementation checkpoint records:

- commands run;
- packages and fixtures covered;
- seeds and schedules used;
- skipped tests and reason;
- capsule replay count;
- failures or flakiness;
- race result;
- line-budget delta.
