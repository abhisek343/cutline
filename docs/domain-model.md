# Domain model

The canonical model lets native Go and Temporal adapters describe a run without sharing runtime-specific types.

## Core records

- **Campaign**: target command, exploration bounds, and contracts.
- **Run**: one top-level campaign execution.
- **Attempt**: one schedule evaluated within a run.
- **Session**: the adapter connection or workflow execution that produced events.
- **Checkpoint visit**: one arrival at a named controlled point.
- **Cancellation**: request, delivery, and observation boundaries.
- **Task**: target or activity work with a parent relationship and terminal state.
- **Effect**: a declared, attempted, committed, failed, compensated, or unknown external action.
- **Event**: one ordered observation of a lifecycle transition.
- **Failure signature**: a stable description of a violation used by minimization and replay.

## States

A run moves from planning to running, draining, frozen, then pass, violation, or inconclusive. A task and effect may have only one terminal outcome. A committed effect cannot later fail, and an unknown effect cannot be treated as failed.

## Identity and order

Run, attempt, session, and entity IDs are deterministic content IDs. Events are ordered by the ingest loop, not by adapter wall clocks alone. Per-session local sequence numbers expose gaps and duplicates.

The model represents facts. It does not infer that an event did not happen because the adapter did not send one.
