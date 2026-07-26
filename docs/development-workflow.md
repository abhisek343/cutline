# Development workflow

## Prerequisites

- Go 1.26
- Docker, for PostgreSQL and Temporal integration tests

Run `go mod download` once after cloning. The standard local check is:

```sh
make check
```

It formats Go files, runs `go vet`, unit tests, and the race detector.

## Before changing behavior

Read the relevant package tests and the matching document. For a change that affects cancellation, effect timing, or evidence completeness, also read [contracts](contracts.md).

Keep a change small enough to run through the full loop:

```text
instrument -> run -> freeze evidence -> evaluate -> minimize -> replay
```

Do not add a new adapter or infrastructure service just to make a feature easier to demonstrate. An architecture decision is appropriate when a change alters persistence, the public SDK, adapter compatibility, or the meaning of canonical evidence.

## Test commands

```sh
make check
make integration
make temporal-integration
make release
```

`make integration` and `make temporal-integration` require Docker. CI runs both independently.

For a native manual probe, build the CLI and run the faulty and clean checkout campaigns from the README. The faulty campaign must return a violation; the clean campaign must pass.

## Review checklist

Before opening a change:

- Run focused tests, then `make check`.
- Add a regression test for the bug or edge case.
- Run the real fixture affected by the change.
- Replay any new failure capsule and confirm its signature.
- Check that missing or ambiguous evidence becomes inconclusive, not pass.
- Keep docs and campaign examples in sync.
- Review `git diff` for generated files and unrelated formatting.

## Database changes

Migrations are embedded and applied by the integration tests. Add a new migration; do not edit an applied one. Test both a fresh database and repeated application of the migration.

## Safety

Campaigns are local-development tools. Do not extend them to run against production dependencies, remote Temporal endpoints, or unredacted payload capture without an explicit design review.
