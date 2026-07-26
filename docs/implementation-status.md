# Implementation status

This file records the verified implementation checkpoint. A slice is marked
complete only after its focused tests, the fast package gate, and a relevant
manual probe pass. PostgreSQL container execution is delegated to the CI job
because the local environment has no Docker provider.

| Slice | Scope | Status |
|---:|---|---|
| 1 | Go foundation | Complete |
| 2 | Canonical identities and events | Complete |
| 3 | State machines | Complete |
| 4 | Campaign and CLI foundation | Complete |
| 5 | Public instrumentation SDK | Complete |
| 6 | Local control protocol | Complete |
| 7 | Native process adapter | Complete |
| 8 | Single-cut scheduler | Complete |
| 9 | Checkout benchmark fixture | Complete |
| 10 | First complete demonstration | Complete |
| 11 | PostgreSQL schema and migration guardrails | Complete; CI integration required |
| 12 | Durable evidence ingestion and freeze | Complete; CI integration required |
| 13 | Causal graph and effect reconciliation | Complete; CI integration required |
| 14 | CEL contract engine | Complete |
| 15 | Checkpoint discovery and single-cut enumeration | Complete |
| 16 | Boundary-pair and bounded-prefix search | Complete |
| 17 | Stable failure signatures | Complete |
| 18 | Failure minimizer | Complete |
| 19 | Failure-capsule builder | Complete |
| 20 | Replay engine | Complete |
| 21 | Static report | Complete |
| 22 | Temporal workflow adapter | Complete; SDK integration required |
| 23 | Temporal activity adapter | Complete; SDK integration required |
| 24 | Benchmark and release gate | Complete |

The current native manual probes must continue to show a faulty checkout as a
contract violation and a clean checkout as a pass. In both cases the typed view
must be complete and must expose cancellation ordering, drain evidence, and
authoritative-effect reconciliation.

Slice 18 also verifies that native candidate execution uses the same explicit
release-prefix policy as exploration. The deterministic minimizer only accepts
a candidate when it reproduces the exact stable failure-signature digest, then
confirms the resulting schedule within a bounded attempt budget.

Slice 19 packages a stable violation into a versioned directory with canonical
events, derived effects and graph data, the minimized schedule, checksums, a
redacted target/dependency record, and escaped static report data. Import
validation rejects traversal, symlinks, tampered artifacts, and incompatible
schema versions.

Slice 20 validates capsules before reading any executable input, checks target
compatibility, executes the minimized schedule through the native runner, and
labels exact, comparative, different-signature, and non-reproduced outcomes.

Slice 21 renders validated capsule data as JSON or escaped standalone HTML.
The report command never executes the target and refuses to overwrite an
existing output file.

Slice 22 adds the isolated Temporal history DTO boundary and workflow lifecycle
translator. Because this module has no Temporal SDK dependency in the current
repository, CI integration with a pinned local Temporal server remains an
explicit follow-up rather than a hidden claim.

Slice 23 adds typed activity-effect history construction and correlation by
activity/effect identity. Unknown dependency outcomes are represented as
canonical `effect.unknown` transitions and invalid outcome labels are rejected.

Slice 24 runs the seeded faulty checkout 20 times, requires at least 19 exact
failure-signature matches, verifies the corrected checkout passes without
signatures, enforces the 14,400-line budget, and exposes the combined `make
release` gate. Temporal/PostgreSQL external-container execution remains an
explicit CI dependency.
