# Architecture

Cutline is a modular Go application with one CLI and a small SDK that targets embed in Go tests. The model package is deliberately independent of adapters, storage, and the Temporal SDK.

## Packages

```text
cmd/cutline/              CLI entry point
pkg/cutline/              public checkpoint and effect API
internal/campaign/        campaign loading and validation
internal/model/           canonical IDs, events, and state
internal/control/         local SDK control protocol
internal/scheduler/       checkpoint release decisions
internal/explorer/        bounded schedule generation
internal/ingest/          ordered event ingestion and freezing
internal/evidence/        effects, tasks, and causal views
internal/contracts/       CEL and built-in checks
internal/minimize/        failure-preserving reduction
internal/capsule/         capsule creation and validation
internal/replay/          capsule replay
internal/report/          HTML and JSON rendering
internal/ledger/          PostgreSQL persistence
internal/adapters/native/ Go test target adapter
internal/adapters/temporal/ local Temporal history adapter
```

Dependencies point toward `internal/model`. Runtime adapters translate their own events into model events; the model never imports an adapter or database package.

## Native execution

1. The CLI loads a campaign and starts the configured Go test target.
2. The target registers with Cutline over a local control connection.
3. When it reaches `cutline.Point`, the scheduler either releases it or requests cancellation.
4. The target reports checkpoints, tasks, cancellation boundaries, and effects.
5. After the target drains, Cutline freezes evidence and evaluates the configured contracts.
6. A stable violation may be minimized, packaged, replayed, and reported.

The scheduler controls only declared checkpoint releases. It does not control the Go runtime scheduler.

## Temporal

The Temporal adapter reads completed history from a local, loopback Temporal server and translates workflow and activity lifecycle events into the same model. It keeps unsupported history and unknown external effect outcomes explicit rather than guessing a result.

Temporal history ingestion is read-only. Worker instrumentation and campaign-driven checkpoint control are not implemented yet.

## Ownership and ordering

The scheduler owns schedule state. One ingest loop assigns canonical event order. Contract evaluation begins only after evidence is frozen. Capsule and report code read immutable evidence.

If the adapter loses evidence, a drain times out, or an effect result is unknown, the affected contract is inconclusive rather than passing.

## Further reading

- [System design](system-design.md) explains the data flow in more detail.
- [Domain model](domain-model.md) lists the canonical records and states.
- [ADR index](adr/README.md) records the decisions behind these boundaries.
