# 0005: Versioned Runtime Adapter Boundary

- Status: Accepted
- Date: 2026-07-25

## Context

Native Go contexts and Temporal workflows expose different cancellation delivery,
history, task, and quiescence semantics. Letting either model leak into the core
would make contracts runtime-specific and make future adapters difficult.

## Decision

Define a versioned runtime adapter boundary.

An adapter:

- declares capabilities;
- launches or connects to a target;
- delivers supported cancellation triggers;
- translates runtime facts into canonical events;
- identifies authoritative histories and effects;
- determines adapter-specific drain status;
- records target and dependency versions;
- prepares exact replay.

The core owns canonical semantics and refuses contracts whose required
capabilities are unavailable.

Temporal SDK types remain inside `internal/adapters/temporal`. Native process
details remain inside `internal/adapters/native`.

## Consequences

### Positive

- one contract language works across runtimes;
- missing observations become explicit capability gaps;
- runtime SDK upgrades are isolated;
- new adapters do not fork the engine.

### Negative

- the canonical model must represent semantic differences without flattening
  them incorrectly;
- adapter conformance testing is required;
- some contracts are inconclusive on weaker adapters;
- version compatibility adds metadata.

## Alternatives considered

- Separate native and Temporal engines: rejected because evidence, contracts,
  minimization, and reports would diverge.
- Lowest-common-denominator events only: rejected because important
  runtime-specific evidence would be lost.
- Temporal-first core model: rejected because native Go is an equal target and
  future runtimes should remain possible.

## Revisit when

At least two implemented adapters demonstrate that the interface cannot express
a required behavior. Extend capability versions rather than inserting
runtime-specific conditionals into the core.
