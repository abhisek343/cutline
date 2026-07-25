# 0003: CEL over a Typed Frozen Evidence Model

- Status: Accepted
- Date: 2026-07-25

## Context

Cancellation correctness is domain-specific. The engine cannot hard-code whether
a charge, notification, reservation, or compensation is allowed. Contracts need
to be declarative, deterministic, embeddable, bounded, and safe to evaluate.

## Decision

Use Common Expression Language (CEL) for user-defined contracts.

Evaluate contracts only against a frozen, typed canonical evidence view. Provide
versioned pure helpers for causality, timing boundaries, task ancestry, effect
commitment, compensation, grouping, and resource release. Disable I/O and apply
cost limits.

Run built-in evidence-integrity checks before CEL.

## Consequences

### Positive

- contracts remain configuration rather than compiled plugins;
- deterministic and bounded evaluation is possible;
- typed environments improve errors and evolution;
- the same contracts work across native Go and Temporal adapters.

### Negative

- CEL helper design becomes a public semantic API;
- complex temporal assertions may be awkward;
- environment versioning and migration require care;
- poor evidence cannot be repaired by expressions.

## Alternatives considered

- Hard-coded Go assertions: too inflexible for user business rules.
- Embedded JavaScript: larger security and determinism surface.
- Rego: capable, but broader policy concepts and runtime weight are unnecessary
  for the first release.
- SQL assertions: couples contracts to storage and makes portability harder.
- Custom DSL: high design and tooling cost.

## Revisit when

Real benchmark contracts cannot be expressed clearly or CEL evaluation becomes a
measured bottleneck. Any replacement must preserve typed, pure, versioned,
bounded evaluation.
