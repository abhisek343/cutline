# Cutline

Cutline is a local test tool for finding work that continues after a Go context or Temporal workflow has been cancelled. It pauses an instrumented target at named checkpoints, tries a bounded set of cancellation schedules, and checks the resulting evidence against business rules.

It is useful when "the request returned cancelled" is not enough. A payment, email, retry, or background task may still have continued.

## What it does

- Controls explicitly instrumented checkpoints in Go tests.
- Records cancellation, task, and effect events in one canonical timeline.
- Evaluates CEL contracts such as "do not charge after cancellation".
- Reduces a failing schedule and writes a replayable failure capsule.
- Renders capsules as JSON or a standalone HTML report.
- Reads completed workflow history from a local Temporal server.

Cutline does not prove an arbitrary concurrent program correct. It explores only the checkpoint schedules declared by the target.

## Quick start

Cutline currently targets Go 1.26 on Linux. Build the CLI and run the included checkout fixture:

```sh
go build -o ./bin/cutline ./cmd/cutline

# Expected: exits 2 and reports no-charge-after-cancel.
./bin/cutline run --campaign test/fixtures/checkout/campaign-faulty.yaml

# Expected: exits 0.
./bin/cutline run --campaign test/fixtures/checkout/campaign-clean.yaml
```

The faulty fixture intentionally starts a charge after cancellation; the clean fixture stops before creating the effect.

## Instrumenting a Go target

Add checkpoints where cancellation timing matters, then record external side effects with a stable idempotency key and an authoritative evidence source.

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

A campaign selects the target, exploration bound, and contracts:

```yaml
apiVersion: cutline.dev/v1alpha1
kind: Campaign
name: checkout

target:
  adapter: go-test
  command: ["go", "test", "./test/fixtures/checkout", "-run", "TestCheckout"]

exploration:
  strategy: checkpoint
  maxSchedules: 200

contracts:
  - name: no-charge-after-cancel
    severity: critical
    boundary: cancellation-observed
    expression: >
      !effects.exists(e,
        e.kind == "payment.charge" &&
        e.committed &&
        e.startedAfter(cancel.observedAt))
```

## Working with failures

For a stable violation, create and replay a capsule:

```sh
cutline minimize --campaign cutline.yaml --output capsules/checkout-cancel
cutline replay capsules/checkout-cancel
cutline report capsules/checkout-cancel --format html --output report.html
```

A capsule contains the minimized release schedule, canonical events, derived effect and causal data, contract result, checksums, and redacted replay metadata. See [failure capsules](docs/failure-capsules.md) for the layout.

## Temporal

`cutline temporal inspect` reads a completed execution from a local Temporal server and converts its history to Cutline events:

```sh
cutline temporal inspect --workflow-id example --namespace default
```

The command accepts loopback endpoints only. It is evidence ingestion, not yet a checkpoint-driven campaign runner for arbitrary Temporal workers. Temporal history also cannot by itself prove whether an external dependency committed an effect; workers must provide that evidence.

## Development

```sh
make check                 # format, vet, unit tests, race detector
make integration           # PostgreSQL integration tests (Docker required)
make temporal-integration  # local Temporal integration test (Docker required)
make release               # check, line budget, reference benchmark, build
```

For a local PostgreSQL instance, run `docker compose up -d postgres`. The database listens on `127.0.0.1:54329`.

## Project status

The native Go workflow is complete: discovery, bounded exploration, contract evaluation, minimization, replay, and reporting all run against the reference fixture. PostgreSQL-backed evidence and local Temporal history ingestion are also implemented. The next Temporal increment is worker instrumentation and checkpoint-driven campaign execution.

## Documentation

- [Getting started and project map](docs/README.md)
- [Contracts](docs/contracts.md)
- [Architecture](docs/architecture.md)
- [Failure capsules](docs/failure-capsules.md)
- [Development guide](docs/development-workflow.md)
- [Testing](docs/testing-strategy.md)
- [Architecture decisions](docs/adr/README.md)

## License

No license has been selected. Until one is added, the repository does not grant permission to redistribute or reuse the source.
