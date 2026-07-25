# 0001: Modular Monolith with a Small Public SDK

- Status: Accepted
- Date: 2026-07-25

## Context

Cutline needs scheduling, event ingest, persistence, contracts, minimization,
reports, and runtime adapters. Splitting these into services would add network
failure modes and operational code before the product loop is proven.

Targets also need a stable instrumentation API that must not expose internal
infrastructure.

## Decision

Build one `cutline` CLI process as a modular monolith. Keep packages separated by
domain responsibility and enforce inward dependencies toward the canonical
model.

Expose a small Go SDK in `pkg/cutline` containing only instrumentation and
session-facing contracts. Keep PostgreSQL, Cobra, CEL, Temporal, reports, and
exploration internals out of that public package.

## Consequences

### Positive

- one process is easier to reproduce and debug;
- transactional evidence and schedule ownership remain clear;
- lower operational and line-count cost;
- internal modules can evolve before public stabilization;
- target instrumentation stays small.

### Negative

- heavy runs cannot scale components independently;
- package-boundary discipline must be enforced in review;
- a coordinator crash affects the whole run.

## Alternatives considered

- Microservices for scheduler, ingest, and reports: rejected as premature.
- One unstructured package: rejected because semantic boundaries would blur.
- SDK containing the engine: rejected because target dependency weight would be
  excessive.

## Revisit when

Measured single-process limits prevent required local workloads or independent
failure isolation becomes necessary. Any split must preserve one canonical event
and ordering model.
