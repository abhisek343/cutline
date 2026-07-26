# Project scope

Cutline helps Go teams test one specific failure mode: work that crosses a cancellation boundary when it should have stopped.

The intended users are backend, platform, and reliability engineers who already have Go services or Temporal workflows and need a reproducible way to test cancellation-sensitive business effects.

## The workflow

1. Add explicit checkpoints around cancellation-sensitive code.
2. Describe the target and business rule in a campaign.
3. Run the bounded schedule search locally.
4. Inspect, minimize, and replay a violation.

The useful output is not a generic race warning. It is a small schedule, a named contract, and the effect that violated it.

## Supported scope

The native Go adapter supports checkpoint discovery, bounded exploration, evidence collection, CEL contracts, minimization, replay, and static reporting. PostgreSQL can persist evidence. The Temporal adapter runs local, command-launched workers through the same checkpoint protocol, cancellation schedules, contract evaluation, minimization, and replay path as native targets. It also reads local completed history and translates it into the same evidence model.

## Deliberate limits

Cutline is not a production testing tool, a general chaos framework, or a replacement for the Go race detector. It does not control arbitrary goroutine scheduling or infer an external dependency result from the absence of an event.

The first release stays local-first: no hosted service, remote workflow control, automatic binary instrumentation, or multi-host coordinator.
