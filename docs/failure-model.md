# Failure Model

## Purpose

The failure model defines which cancellation problems Cutline attempts to expose
and how infrastructure failures affect verdicts.

## Target failure classes

### Post-cancel side effect

A prohibited effect is attempted or committed after the contract's chosen
cancellation boundary.

Example: a checkout handler observes cancellation, then starts a payment charge.

### Commit/acknowledgement ambiguity

The dependency commits an effect, but the caller sees cancellation or timeout
before acknowledgement and incorrectly retries.

Example: the first email send succeeds, the response is lost, and a second send
commits.

### Orphan work

A registered child task continues after its parent returns or after the run drain
deadline.

Example: a goroutine writes audit data after the request has terminated.

### Missing cleanup

A resource acquired before cancellation is never released, expired, or safely
transferred.

Example: a lock or inventory reservation remains held.

### Invalid compensation

A committed effect that requires compensation is not compensated, is compensated
too late, or is compensated more than allowed.

### Duplicate effect

Retries or concurrent children commit the same logical effect more than the
contract permits.

### Broken propagation

A child does not receive or observe inherited cancellation, or receives an
unrelated episode.

### Cancellation masking

The target returns success despite a cancellation path that should dominate, or
returns cancellation after committing a result that the API contract calls
successful.

### Temporal-specific races

- activity commits before heartbeat observes cancellation;
- disconnected context permits activity completion;
- workflow catches cancellation but starts new prohibited work;
- child workflow cancellation policy leaves unexpected work running;
- retry policy duplicates a non-idempotent activity effect.

## Injection model

Version 1 injects:

- explicit cancellation at a reached checkpoint;
- deterministic deadline expiration through an adapter-controlled test clock;
- parent cancellation while one or more children are blocked;
- Temporal workflow cancellation at a workflow checkpoint;
- Temporal activity cancellation around heartbeat/effect boundaries.

Network partitions, process kills, clock jumps, and arbitrary dependency faults
are out of scope unless a fixture uses them only to model cancellation outcome
ambiguity.

## Race windows

Every effect fixture should make these windows independently reachable:

```text
before intent
intent -> attempt
attempt -> dependency acceptance
acceptance -> commitment
commitment -> acknowledgement
acknowledgement -> caller record
caller record -> response
```

Cancellation at each window can have different correct behavior. Contracts, not
Cutline defaults, decide which outcomes are permitted.

## Tool and infrastructure failures

| Failure | Verdict treatment |
|---|---|
| Invalid campaign or CEL | `invalid` |
| Unsupported adapter capability | `inconclusive` |
| SDK/control disconnect | `inconclusive` or operational failure |
| Event sequence gap | `inconclusive` |
| PostgreSQL unavailable | operational failure; stop schedule |
| Target crash | contract-dependent violation or inconclusive |
| Expected injected target stop | evaluate according to campaign |
| Drain timeout | violation for explicit liveness rule, otherwise inconclusive |
| Cleanup failure | operational failure separate from target verdict |
| Capsule checksum failure | reject replay |
| Replay signature differs | `flaky` or `not reproduced` |

## False-positive controls

- compare explicit semantic boundaries, not wall-clock proximity;
- require authoritative effect outcome when a contract depends on commitment;
- freeze evidence before evaluation;
- preserve contract and adapter versions;
- confirm minimized candidates repeatedly;
- distinguish target bugs from harness failures.

Cutline v1 failure signatures normalize the contract name and version, failure
class, primary entity kind, cancellation trigger, causal-path shape, and
adapter/schema major versions. Run IDs, timestamps, generated entity IDs, and
incidental target output are intentionally excluded.

## False-negative controls

- incomplete evidence never passes;
- capability requirements are explicit;
- benchmark fixtures include known bugs;
- dependency state is reconciled with SDK events;
- search bounds and unexplored schedules appear in reports;
- raw unregistered concurrency is reported as a coverage limitation.

## Failure severity

Suggested levels:

- `critical` — money movement, destructive action, security boundary;
- `high` — duplicate external effect, leaked durable resource, orphan workflow;
- `medium` — delayed cleanup, incorrect terminal status, bounded excess work;
- `low` — diagnostic or efficiency invariant.

Severity belongs to the campaign contract, not the engine.
