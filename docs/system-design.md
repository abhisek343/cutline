# System design

This page describes the path from an instrumented target to a contract result. See [architecture](architecture.md) for package ownership.

## Evidence path

```text
campaign -> adapter -> control/events -> ingest -> frozen evidence
                                              -> contracts -> violation
                                              -> minimize -> capsule/report
```

The adapter is responsible for collecting runtime facts. Ingest assigns a canonical sequence and freezes the run once the target has returned and relevant work has drained. The contract engine only reads that frozen view.

## Control and scheduling

A native target connects to the CLI over a private local control endpoint. It reports checkpoints and blocks at them until Cutline releases the visit or injects cancellation. This controls a declared test boundary, not the Go scheduler.

A schedule contains cancellation and release decisions. Exploration is bounded by the campaign's strategy and limits. The scheduler is the only writer of schedule state.

## Events and persistence

Events carry run, attempt, session, entity, parent, type, timestamp, and attributes. Ingest rejects identity and sequence problems. PostgreSQL persistence records the same canonical event model and freezes evidence transactionally.

Effect evidence must identify its source. A committed external effect is authoritative only when the adapter or dependency reports it; a missing result remains unknown.

## Temporal boundary

The Temporal adapter runs a command-launched local worker for each campaign schedule. Its activities report through Cutline's private control endpoint and use the local SDK to cancel the workflow when Cutline injects cancellation. The history reader remains available for inspection. External effect commitments still require worker-provided authoritative evidence.

## Failure handling

A lost control connection, sequence gap, timeout during drain, unsupported history event, or unknown effect outcome makes evidence incomplete. Contracts depending on that evidence are inconclusive. This is preferable to reporting a pass that the run did not earn.

## Security

Campaign commands run without shell interpolation. Capsule imports validate schema, checksums, paths, and symlinks. Reports are static escaped output. Local-only adapters reject remote endpoints by design.
