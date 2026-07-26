# Implementation status

## Available today

The native Go adapter supports the complete local workflow:

- campaign loading and validation;
- checkpoint discovery and bounded schedule exploration;
- cancellation control through the local SDK protocol;
- canonical event ingestion and optional PostgreSQL persistence;
- typed CEL contracts and built-in integrity checks;
- stable failure signatures, minimization, capsules, replay, and reports.

The checkout fixture is the reference demonstration. The faulty version produces a contract violation; the corrected version passes. The release test repeats the faulty replay 20 times and requires at least 19 exact signatures.

## Temporal

The Temporal adapter has been verified against a local server. It reads completed workflow and activity history through the Go SDK and translates it to canonical events. Unsupported history and external effect outcomes that cannot be verified are kept incomplete or unknown.

It does not yet control checkpoints in arbitrary Temporal workers. That needs a worker instrumentation protocol and a local campaign runner; see the [roadmap](roadmap.md).

## Verification

`make check` covers formatting, vet, unit tests, and the race detector. PostgreSQL and Temporal container tests run in CI and can be run locally with Docker. `make release` adds the line budget, reference benchmark, and production build.
