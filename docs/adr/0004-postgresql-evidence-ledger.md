# 0004: PostgreSQL as the Authoritative Evidence Ledger

- Status: Accepted
- Date: 2026-07-25

## Context

Cutline needs ordered append-heavy evidence, state-machine constraints,
cross-entity references, immutable snapshots, minimization history, and queries
for reports. In-memory evidence is useful for the first slice but insufficient
for crash diagnosis and reproducible artifacts.

## Decision

Use PostgreSQL as the authoritative run and evidence ledger.

Persist validated canonical events and normalized entities with relational
constraints. Freeze a run before evaluation. Export portable JSON/JSONL from the
frozen snapshot. Use Testcontainers for integration tests and Docker Compose for
local development.

PostgreSQL stores Cutline evidence; fixture dependencies remain authoritative for
their actual business effects and are reconciled through adapters.

## Consequences

### Positive

- durable and inspectable evidence;
- strong uniqueness and transition constraints;
- transactional freeze and evaluation snapshots;
- practical graph and report queries;
- mature local tooling.

### Negative

- local infrastructure is required;
- schema migration and compatibility add work;
- database interruption becomes a harness failure mode;
- PostgreSQL is not itself proof that an external effect occurred.

## Alternatives considered

- SQLite: attractive for portability, but concurrent ingest and the intended
  fixture environment already need stronger integration testing.
- Embedded key-value store: poor fit for relational integrity and ad hoc
  diagnosis.
- Event files only: weak transactional constraints and crash recovery.
- NATS/Kafka: unnecessary durable-stream infrastructure for a local-first tool.

## Revisit when

A self-contained distribution becomes a release blocker. A portable backend may
be added behind the ledger interface only if it preserves evidence and freeze
semantics.
