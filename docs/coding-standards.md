# Coding Standards

## Go baseline

- Use the supported stable Go version pinned by the repository toolchain.
- Keep `gofmt` clean.
- Prefer the standard library until a dependency removes measured complexity.
- Return explicit errors; do not use panic for expected target or campaign
  failures.
- Wrap errors with operation and stable category while preserving `errors.Is`
  and `errors.As`.
- Pass `context.Context` as the first parameter for cancellable operations.
- Never store contexts in long-lived structs.

## Package design

- Packages own one coherent responsibility.
- Keep public API in `pkg/cutline` intentionally small.
- Do not export types only to make tests convenient.
- Define interfaces at the consumer boundary.
- Avoid `util`, `common`, and cyclic domain abstractions.
- Keep canonical model code free of database, CLI, and runtime SDK imports.

## Cancellation

- Check and propagate the cause where semantics require it.
- Do not convert all cancellation into generic failure.
- Make cleanup deadlines independent when cleanup must continue safely.
- Register child work before starting it.
- Never launch an untracked goroutine in core execution paths.
- Make repeated cancellation idempotent.
- Tests must control synchronization through checkpoints, not sleeps.

## Concurrency

- Document goroutine ownership and termination.
- Prefer one owner for mutable scheduler state.
- Close channels only from their owning sender.
- Bound queues and worker counts.
- Treat timeout as a safety valve, not deterministic coordination.
- Include race tests for cancellation/release and shutdown paths.

## Data and events

- Use opaque typed IDs.
- Use enums with explicit unknown values.
- Validate state transitions centrally.
- Keep accepted evidence immutable.
- Use UTC wall time for display and monotonic duration for local measurement.
- Do not derive cross-process causal order from timestamps.
- Version externally persisted payloads.

## Database

- Use explicit transactions and isolation requirements.
- Include `run_id` in run-owned keys and queries.
- Make ingest idempotent by session and local sequence.
- Prefer append-only evidence after acceptance.
- Keep migrations forward-only until a release policy exists.
- Test constraints in PostgreSQL, not only with mocks.

## CEL contracts

- Expose typed, immutable values.
- Keep helpers pure and deterministic.
- Set expression cost limits.
- Reject unsupported major environments.
- Avoid raw JSON traversal when a typed field can exist.

## Logging

- Use structured logs with run and attempt identity.
- Do not log secrets, full environment, authorization headers, or unrestricted
  payloads.
- Logs aid diagnosis but never substitute for canonical evidence.

## Testing

- Prefer table-driven unit tests and explicit fixture assertions.
- Use deterministic seeds and stable IDs.
- Avoid sleeps; use barriers and acknowledgements.
- Test invalid and incomplete evidence.
- Keep golden files small and schema-focused.
- Clean external resources by run ID.

## Dependencies

Every new dependency needs:

- purpose and alternatives considered;
- maintenance and license check;
- security and transitive-dependency review;
- version pin or reproducible resolution;
- line/complexity tradeoff.

Core planned dependencies are Cobra, CEL-Go, Temporal Go SDK, a PostgreSQL driver,
and Testcontainers for Go.

## Comments and documentation

Comments explain invariants, ownership, ordering, and non-obvious failure
semantics. Do not narrate straightforward code. Public APIs include behavior
under cancellation and concurrency.
