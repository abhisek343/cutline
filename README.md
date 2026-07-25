# Cutline

**Find the business actions that keep happening after cancellation.**

Cutline is a deterministic cancellation-correctness tester for Go services and
Temporal workflows. It injects cancellation at named checkpoints, records what
the program does next, evaluates business-level invariants, minimizes a failure,
and exports a replayable failure capsule.

Cancellation in Go is cooperative. Calling `cancel()` or cancelling a Temporal
workflow does not prove that every goroutine, activity, retry, transaction, or
external side effect stopped safely. A request can return "cancelled" while a
payment is still charged, an email is still sent, a lock is leaked, or a retry
continues in the background. Cutline is designed to expose those failures as
small, deterministic counterexamples.

## Status

Cutline is in the design-baseline phase. The repository currently contains the
contracts and implementation plan that code must satisfy. The first build target
is one end-to-end native Go fixture, not a broad framework.

## What a test looks like

The target service adds explicit instrumentation around cancellation-sensitive
boundaries:

```go
cutline.Point(ctx, "before-charge")

effect := cutline.Effect(ctx, cutline.EffectSpec{
    Kind:           "payment.charge",
    IdempotencyKey: orderID,
})

err := gateway.Charge(ctx, orderID, amount)
effect.Complete(err)
```

A campaign tells Cutline where cancellation may be injected and what must remain
true:

```yaml
apiVersion: cutline.dev/v1alpha1
kind: Campaign

target:
  adapter: go-test
  command: ["go", "test", "./test/fixtures/checkout", "-run", "TestCheckout"]

exploration:
  strategy: checkpoint
  maxSchedules: 200

contracts:
  - name: no-charge-after-cancel
    severity: critical
    expression: >
      !effects.exists(e,
        e.kind == "payment.charge" &&
        e.committed &&
        e.startedAfter(cancel.observedAt))
```

The intended CLI flow is:

```text
cutline doctor
cutline run --campaign cutline.yaml
cutline minimize <run-id>
cutline replay capsules/<failure-id>
cutline report <run-id> --format html
```

## Core result

A failing run produces a **failure capsule** containing:

- the exact tool and target versions;
- campaign and deterministic seed;
- injected cancellation point and release schedule;
- ordered checkpoint, cancellation, task, and effect events;
- causal graph and side-effect ledger;
- violated invariant and stable failure signature;
- minimized schedule;
- one-command replay instructions.

Cutline finds counterexamples within a bounded, instrumented search space. It
does not claim to prove arbitrary concurrent programs correct.

## Planned stack

| Area | Choice |
|---|---|
| Language | Go |
| Workflow adapter | Temporal Go SDK and local Temporal server |
| Evidence ledger | PostgreSQL |
| Contract language | Common Expression Language (CEL) |
| CLI | Cobra |
| Integration tests | Testcontainers for Go |
| Local environment | Docker Compose |
| Reports | Static HTML plus JSON/JSONL evidence |

## Documentation

- [Documentation index](docs/README.md)
- [Product definition](docs/product.md)
- [System design](docs/system-design.md)
- [Architecture and execution flows](docs/architecture.md)
- [Domain model](docs/domain-model.md)
- [Contracts and invariants](docs/contracts.md)
- [Failure model](docs/failure-model.md)
- [Failure capsule format](docs/failure-capsules.md)
- [Testing strategy](docs/testing-strategy.md)
- [Roadmap and line budget](docs/roadmap.md)
- [Development workflow](docs/development-workflow.md)
- [Coding standards](docs/coding-standards.md)
- [Architecture decisions](docs/adr/README.md)
- [AI coding instructions](AGENTS.md)
- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)

## Scope guardrail

The target repository size for the first credible release is approximately
**14,400 meaningful lines**, including tests and excluding generated or vendored
code. New subsystems must displace equivalent complexity or be justified by an
accepted architecture decision.

## License

No license has been selected yet. Until a license is added, the source is not
granted for redistribution or reuse.
