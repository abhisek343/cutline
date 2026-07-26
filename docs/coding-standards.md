# Coding standards

## Go

Use standard Go formatting and package conventions. Keep exported APIs small and document exported identifiers when the name does not explain the contract.

Prefer values and explicit return errors over hidden mutable state. Keep the public SDK independent of storage, CLI, and runtime-specific dependencies.

## Events and cancellation

Treat cancellation request, delivery, observation, target return, drain completion, and effect commitment as separate facts. Do not turn missing evidence into a negative observation.

Canonical events need stable identities and deterministic ordering. Adapter code may translate runtime details, but it must not redefine event semantics.

## Concurrency

State ownership should be clear from the package API. Avoid sharing scheduler state across goroutines. Make cleanup idempotent and use bounded context timeouts for external work.

Run the race detector for changes involving control, scheduling, ingestion, or persistence.

## Tests

Write tests at the lowest useful layer, then add a fixture or integration test for cross-package behavior. A failure minimizer test must assert the signature, not merely a non-zero exit.

Use the intentionally faulty and corrected fixtures to demonstrate a real contract difference. Tests that require Docker should be tagged or invoked through the matching Make target.

## Dependencies and docs

Avoid adding dependencies for simple standard-library work. New runtime or persistence dependencies need an ADR when they affect deployment, compatibility, or the package boundary.

Update the README when commands or supported scope change. Put detailed design rationale in an ADR, not in code comments.
