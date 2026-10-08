# 0006: Local-only Temporal history ingestion

- Status: Accepted
- Date: 2026-07-26

## Context

The canonical Temporal adapter was tested only with DTO fixtures. Cutline needs
to verify that its adapter can read a real Temporal workflow history without
turning a local testing tool into a remote workflow control plane.

## Decision

Add a Temporal Go SDK client behind `cutline temporal inspect`. It accepts only
loopback `host:port` endpoints, reads one workflow execution history, and
translates it through the existing canonical adapter. A pinned Docker Compose
fixture with PostgreSQL and Temporal verifies a real worker cancellation in CI.

The command is read-only. It does not claim to schedule arbitrary Temporal
workers or infer business-effect commits from workflow history.

## Consequences

### Positive

- verifies the SDK and server boundary over real gRPC;
- keeps remote and production endpoints outside the product boundary;
- preserves the canonical model and honest incomplete-evidence semantics;
- uses a reproducible local fixture without adding a distributed coordinator.

### Negative

- a separate container integration gate is required;
- local checkpoint campaigns launch a command worker using the native control protocol; this does not provide arbitrary Temporal history replay;
- external effects still require worker-provided authoritative evidence.

## Alternatives considered

- DTO-only tests: rejected because they do not verify SDK/server compatibility.
- a remote Temporal Cloud connector: rejected because it expands the safety and
  credential boundary.
- embedding a Temporal server as a Go dependency: rejected because it creates
  a large, incompatible dependency graph for the CLI.

## Revisit when

Remote or additional workflow engine support is proposed. Local checkpoint
campaigns are now implemented alongside read-only history inspection; the
reference fixture verifies real cancellation propagation before business effects.
