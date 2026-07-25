# 0002: Explicit Checkpoints Instead of Full Scheduler Control

- Status: Accepted
- Date: 2026-07-25

## Context

Cancellation races depend on timing and concurrency. Fully controlling the Go
runtime scheduler would require runtime modification or fragile low-level hooks.
Ordinary randomized stress is easier but produces flaky, hard-to-minimize
failures.

## Decision

Use cooperative, named checkpoints and registered task boundaries. During a
Cutline run, a reached checkpoint blocks until the coordinator releases it or
the adapter applies cancellation semantics. The scheduler controls the order of
these explicit releases and the cancellation injection point.

Cutline will describe this as deterministic checkpoint scheduling within a
bounded instrumented model, never deterministic execution of arbitrary Go code.

## Consequences

### Positive

- schedules are understandable and replayable;
- instrumentation maps to business-sensitive boundaries;
- implementation remains compatible with standard Go;
- minimization can operate on discrete actions;
- users can see what was and was not explored.

### Negative

- bugs outside declared points can be missed;
- instrumentation changes timing and requires adoption effort;
- unregistered goroutines remain partly unobservable;
- schedule coverage is bounded rather than exhaustive.

## Alternatives considered

- Random sleeps and stress loops: rejected because replay and minimization are
  weak.
- Go runtime fork or scheduler hooks: rejected for portability and maintenance.
- OS-level process scheduling: rejected because it does not express semantic
  cancellation boundaries.
- Model checking source code: rejected as a different and much broader product.

## Revisit when

A stable runtime-supported scheduling API becomes available, or benchmark
evidence shows an additional narrowly scoped hook is needed. Explicit points
remain part of the evidence model even if control improves.
