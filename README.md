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

Cutline is under active implementation. The deterministic model, campaign CLI,
native Go SDK/control path, PostgreSQL evidence ledger, typed causal view,
checkpoint discovery, bounded single-cut enumeration, boundary-pair and
bounded-prefix exploration, typed CEL contracts, and the faulty/correct checkout
benchmark are runnable, with stable failure signatures attached to violations
and a bounded deterministic minimizer that preserves those signatures. Stable
violations can now be packaged as checksummed, redaction-aware failure capsules
and replayed with exact-signature comparison. Reports and Temporal support are
being delivered next.

## What a test looks like

The target service adds explicit instrumentation around cancellation-sensitive
boundaries:

```go
if err := cutline.Point(ctx, "before-charge"); err != nil {
    return err
}

effect, err := cutline.BeginEffect(ctx, cutline.EffectSpec{
    Kind:           "payment.charge",
    IdempotencyKey: orderID,
    EvidenceSource: "payment-ledger",
})
if err != nil {
    return err
}
if err := effect.Attempt(ctx); err != nil {
    return err
}

if err := gateway.Charge(ctx, orderID, amount); err != nil {
    return effect.Fail(ctx, err)
}
return effect.Commit(ctx)
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

The current native demonstration is:

```text
go build -o ./bin/cutline ./cmd/cutline
./bin/cutline run --campaign test/fixtures/checkout/campaign-faulty.yaml
./bin/cutline run --campaign test/fixtures/checkout/campaign-clean.yaml
```

The faulty campaign exits `2` with a violation; the clean campaign exits `0`.

## Development

Run the fast local gate with `make check`. PostgreSQL integration tests use a
pinned Testcontainers image:

```text
make integration
```

For manual database work, `docker compose up -d postgres` exposes the local-only
database on `127.0.0.1:54329`. The CI integration job starts its own isolated
container and applies every embedded migration twice to verify idempotency.

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
