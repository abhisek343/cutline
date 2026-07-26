# Contracts

A contract is a CEL expression evaluated after Cutline freezes a run's evidence. It answers one business question, such as whether a payment was committed after cancellation was observed.

A result is one of:

- **pass**: complete evidence satisfies the expression;
- **violation**: complete evidence contains a counterexample;
- **inconclusive**: required evidence is missing or ambiguous;
- **invalid**: the campaign or expression cannot be evaluated.

Cutline never treats missing evidence as a pass.

## Campaign contract

```yaml
contracts:
  - name: no-charge-after-cancel
    version: 1
    severity: critical
    requires:
      - cancellation.observed
      - effects.authoritative_commit
      - drain.registered_tasks
    boundary: cancellation-observed
    expression: >
      !effects.exists(e,
        e.kind == "payment.charge" &&
        e.committed &&
        e.startedAfter(cancel.observedAt))
```

`name`, `severity`, and `expression` are required. Use `requires` when an assertion only makes sense with a particular adapter capability. Use `boundary` for a time-relative assertion; “after cancellation” is not specific enough on its own.

## Available evidence

The CEL environment reads a typed, frozen view:

- `cancel`: cancellation request, delivery, and observation times;
- `run`: terminal and drain state;
- `effects`: declared, attempted, committed, failed, compensated, or unknown effects;
- `tasks`: recorded target and activity work;
- `resources`: acquired and released resources.

Use a committed effect only when its dependency outcome is authoritative. An unknown outcome stays unknown and can make a contract inconclusive.

## Examples

No forbidden effect after observation:

```cel
!effects.exists(e,
  e.kind == "payment.charge" &&
  e.committed &&
  e.startedAfter(cancel.observedAt))
```

Every committed reservation is compensated before the run drains:

```cel
effects
  .filter(e, e.kind == "inventory.reserve" && e.committed)
  .all(e, e.wasCompensatedBefore(run.drainedAt))
```

No child task remains active at drain:

```cel
!tasks.exists(t,
  t.descendsFrom(cancel.targetTask) &&
  !t.isTerminalAt(run.drainedAt))
```

## Evidence rules

Before CEL runs, Cutline checks canonical event order, task lifecycle, cancellation ordering, effect transitions, and schedule integrity. Evaluation is inconclusive when the control channel disconnects, event sequence has a gap, drain times out, a required capability is absent, or a dependency result is unknown.

Contract and evidence schemas are versioned. A newer binary must reject an unsupported major version rather than reinterpret prior evidence.
