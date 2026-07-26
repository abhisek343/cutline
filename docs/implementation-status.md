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
| 19 | Failure-capsule builder | Planned |
| 20 | Replay engine | Planned |
| 21 | Static report | Planned |
| 22 | Temporal workflow adapter | Planned |
| 23 | Temporal activity adapter | Planned |
| 24 | Benchmark and release gate | Planned |

The current native manual probes must continue to show a faulty checkout as a
contract violation and a clean checkout as a pass. In both cases the typed view
must be complete and must expose cancellation ordering, drain evidence, and
authoritative-effect reconciliation.

Slice 18 also verifies that native candidate execution uses the same explicit
release-prefix policy as exploration. The deterministic minimizer only accepts
a candidate when it reproduces the exact stable failure-signature digest, then
confirms the resulting schedule within a bounded attempt budget.
