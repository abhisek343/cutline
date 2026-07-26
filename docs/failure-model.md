# Failure model

Cutline looks for business work that conflicts with a declared cancellation boundary. It is most useful when ordinary unit tests exercise only one happy timing.

## Examples

- A charge commits after the handler observed cancellation.
- An activity continues after a workflow has been cancelled.
- A retry creates a duplicate effect after the caller returned.
- A child task remains live when the run claims to have drained.
- A resource is acquired by cancelled work and never released.
- Compensation is missing after a committed reservation.

## Injection model

The target chooses named checkpoints. Cutline pauses a checkpoint visit, decides whether to release it or inject cancellation, then observes the resulting run. Exploration is bounded and deterministic with respect to the declared release schedule; it is not arbitrary Go schedule control.

## Important distinctions

Cancellation request, delivery, observation, target return, and drain completion are separate events. Effect intent, attempt, commit, acknowledgement, and compensation are separate events too. A contract must name the boundaries it compares.

## Limits

Tool failure, lost evidence, an incomplete drain, or an unknown dependency outcome are not target passes. Cutline reports the result as inconclusive when it cannot support a verdict.
