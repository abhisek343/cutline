# Product Definition

## One-sentence definition

Cutline finds and minimizes business-level side effects that occur incorrectly
during or after cancellation in instrumented Go services and Temporal workflows.

## The problem

Cancellation APIs communicate intent; they do not establish a global stop
boundary. In real systems:

- a goroutine can ignore `ctx.Done()`;
- a child task can outlive its parent;
- a database commit can race with cancellation;
- an HTTP call can succeed after the caller gives up;
- a Temporal activity can commit an effect before it observes cancellation;
- a retry can duplicate an effect;
- cleanup can fail or never run;
- an API can return "cancelled" while business work continues.

Unit tests usually exercise one chosen timing. Stress and race tests vary timing
but rarely express business invariants such as "do not charge after cancellation
was observed." Chaos tools can kill processes or delay networks, but they do not
normally produce a minimized, replayable causal explanation for cancellation
semantics.

## Target users

- Go backend and platform engineers;
- teams using Temporal for durable workflows;
- reliability engineers validating retries, cleanup, and idempotency;
- maintainers of SDKs and middleware that propagate cancellation;
- reviewers investigating rare cancellation races.

## Jobs to be done

1. Define named points where cancellation timing matters.
2. Enumerate a bounded set of cancellation and release schedules.
3. Observe task, effect, and cancellation lifecycles without conflating them.
4. Express business rules over canonical evidence.
5. Reduce a failing schedule to the smallest stable counterexample.
6. Replay and share the failure without reconstructing the original environment
   manually.

## Product loop

```text
Instrument -> Explore -> Observe -> Evaluate -> Minimize -> Replay -> Fix
```

## Differentiation

Cutline's intended contribution is the combination of:

- explicit cancellation checkpoints;
- deterministic release control within an instrumented search space;
- a business side-effect ledger;
- declarative CEL invariants over a causal event graph;
- failure-signature-preserving minimization;
- portable failure capsules.

The novelty claim is deliberately narrow. Cutline is not the first cancellation
API, fault injector, concurrency tester, workflow debugger, or trace viewer. It
earns differentiation only if the integrated loop reliably discovers, minimizes,
and replays failures that ordinary tests miss.

## Success criteria for the first credible release

- Five benchmark fixtures each contain a known cancellation bug.
- Cutline discovers every seeded bug within a documented schedule bound.
- A minimized capsule replays the same failure signature at least 19 of 20 times
  on the reference environment.
- The report identifies the cancellation point, forbidden effect, and causal
  path without requiring raw-log archaeology.
- A clean fixture produces no false violation across its declared campaign.
- Native Go and Temporal targets share the same canonical event and contract
  model.
- The repository remains near the 14,400-line budget.

## MVP

- Linux-first local execution;
- one native Go test adapter;
- one Temporal adapter using a local Temporal server;
- explicit checkpoints and controlled release order;
- explicit and deadline-style cancellation supported by adapters;
- PostgreSQL event/effect ledger;
- CEL contracts;
- bounded checkpoint exploration;
- failure-signature minimization;
- JSON/JSONL capsule and static HTML report;
- Docker Compose and Testcontainers development environment.

## Non-goals for the first release

- proving arbitrary concurrent programs correct;
- controlling every Go scheduler decision;
- replacing the Go race detector;
- testing production systems;
- supporting every workflow engine or language;
- transparent instrumentation of arbitrary binaries;
- distributed multi-host schedule exploration;
- a hosted SaaS control plane;
- a general-purpose chaos platform;
- full payload capture or production observability.

## Product risks

| Risk | Consequence | Required response |
|---|---|---|
| Instrumentation misses an effect | False confidence | Incomplete evidence must be explicit, never a pass |
| Schedules are not replayable | Tool becomes a flaky stress test | Stable IDs, controlled releases, pinned dependencies |
| CEL rules are too low-level | Users cannot express business intent | Typed helpers and documented examples |
| Temporal and Go semantics leak | Canonical model becomes incoherent | Versioned adapter boundary |
| Minimization changes the bug | Misleading capsule | Preserve stable failure signature |
| Scope expands into chaos testing | Project becomes bloated | Enforce non-goals and line budget |

## Naming

**Cutline** refers to the boundary after which work should no longer cross. The
name is not a claim that cancellation is instantaneous; the tool exists precisely
because real cancellation boundaries are negotiated and observable.
