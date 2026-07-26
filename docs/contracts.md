# Contracts and Invariants

## Contract philosophy

Cutline reports only what its evidence supports. Missing observations, unsupported
adapter capabilities, or ambiguous effect outcomes must not be converted into a
pass.

## Result statuses

| Status | Meaning |
|---|---|
| `pass` | Complete required evidence satisfies the rule |
| `violation` | Evidence contains a stable counterexample |
| `inconclusive` | Evidence or capability is insufficient |
| `invalid` | Contract or campaign is malformed or semantically unsupported |

## Built-in structural invariants

These run before user CEL expressions:

### Event integrity

- run, attempt, and session identities match;
- canonical event sequence is contiguous after freeze;
- target-local sequence has no unexplained gap or conflict;
- referenced entities exist;
- schema versions are supported;
- events cannot arrive after evidence freeze.

### Task integrity

- a task has at most one parent;
- parent task exists before child start is accepted;
- terminal transition occurs at most once;
- a completed task cannot later observe cancellation;
- registered tasks have a known terminal or explicit lost state by drain end.

### Cancellation integrity

- delivery cannot precede request in adapter-authoritative order;
- observation cites a delivered or inherited cancellation episode;
- propagation edges cannot form a cycle;
- repeated requests do not overwrite the first observation boundary.

### Effect integrity

- effect identity is unique within a run;
- attempted follows declared intent;
- committed and failed are mutually exclusive for one attempt;
- compensation cites a committed effect;
- an unknown outcome cannot be treated as failed;
- effect evidence source and confidence are recorded.

### Schedule integrity

- each release references a currently blocked point visit;
- action ordinals are contiguous;
- candidate precondition digest matches the observed scheduler state;
- injected cancellation matches the planned trigger.

## Cancellation boundaries

Contracts must name the boundary they use:

- `cancel.requestedAt`;
- `cancel.deliveredAt`;
- `cancel.observedAt`;
- `target.returnedAt`;
- `run.drainedAt`.

"After cancellation" without a boundary is invalid.

## Default invariant vocabulary

### No forbidden effect after observation

```cel
!effects.exists(e,
  e.kind == "payment.charge" &&
  e.committed &&
  e.startedAfter(cancel.observedAt))
```

### No child outlives drain

```cel
!tasks.exists(t,
  t.descendsFrom(cancel.targetTask) &&
  !t.isTerminalAt(run.drainedAt))
```

### Committed effect is compensated

```cel
effects
  .filter(e, e.kind == "inventory.reserve" && e.committed)
  .all(e, e.wasCompensatedBefore(run.drainedAt))
```

### At most one committed effect per idempotency key

```cel
effects
  .filter(e, e.kind == "email.send" && e.committed)
  .groupByIdempotencyKey()
  .all(group, group.size() <= 1)
```

### Acquired resources are released

```cel
resources
  .filter(r, r.owner.descendsFrom(cancel.targetTask))
  .all(r, r.isReleasedAt(run.drainedAt) || r.expiredSafely())
```

The version 1 environment supports the helper forms shown above. The evaluator
rejects unsupported environment versions and runs only against frozen evidence.

## Contract declaration

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

Required fields:

- stable name and version;
- severity;
- capability requirements;
- explicit boundary when time-relative;
- CEL expression;
- optional human remediation.

## Evidence completeness

A contract declares required capabilities. Evaluation is inconclusive when:

- adapter lacks a required transition;
- control channel disconnected;
- sequence gap exists;
- event limit was reached;
- drain timed out while relevant work remained;
- effect outcome is unknown;
- authoritative dependency snapshot failed;
- evidence schema cannot be interpreted.

A contract may explicitly allow selected unknowns, but the allowance is visible
in its definition and report.

## Effect timing

An effect exposes multiple boundaries:

- intent declared;
- attempt started;
- request accepted by dependency;
- effect committed;
- acknowledgement received;
- compensation committed.

The phrase "effect happened after cancellation" must specify which effect
boundary is compared with which cancellation boundary.

## Failure signature contract

Minimization and replay preserve:

- contract name and major version;
- violation class;
- primary offending entity class;
- cancellation trigger class;
- normalized causal path;
- relevant adapter major version.

They need not preserve wall timestamps, event ordinals, run IDs, or incidental
log text.

## Determinism contract

A capsule is stable when the configured confirmation attempts reproduce the same
failure signature. Default release target: 19 of 20 attempts on the pinned
reference environment.

Failure to meet the target produces `flaky`, not `reproduced`.

## Safety contracts

- campaigns cannot silently target non-local dependencies;
- environment variables and payloads are not captured by default;
- report content is escaped;
- imported capsules are integrity-checked before use;
- target command arguments are executed without shell interpolation.

## Compatibility

Contract semantics are versioned independently from syntax. A newer Cutline
binary must reject or explicitly migrate unsupported major versions. It must not
reinterpret old evidence silently.
