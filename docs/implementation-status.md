# Implementation status

## Available today

The native Go adapter supports the complete local workflow:

- campaign loading and validation;
- checkpoint discovery and bounded schedule exploration;
- cancellation control through the local SDK protocol;
- canonical event ingestion and optional PostgreSQL persistence;
- typed CEL contracts and built-in integrity checks;
- stable failure signatures, minimization, capsules, replay, and reports.

The checkout fixture is the reference demonstration. The faulty version produces a contract violation; the corrected version passes. The release gate must replay the saved minimized capsule 20 times and require at least 19 exact signatures, verify text/JSON exit parity, and check report contents. Repeated fresh campaign runs alone do not satisfy that gate.

## Temporal

The Temporal adapter has been verified against a local server. It reads completed workflow and activity history through the Go SDK and translates it to canonical events. Unsupported history and external effect outcomes that cannot be verified are kept incomplete or unknown.

Temporal campaigns control checkpoints in command-launched local workers. Remote workers and remote Temporal endpoints remain out of scope; see the [roadmap](roadmap.md).

## Verification

`make check` covers formatting, vet, unit tests, and the race detector. PostgreSQL and Temporal container tests run in CI and can be run locally with Docker. `make release` adds the reference benchmark and production build.

## Release evidence

All checks must pass on the same reviewed candidate commit. CI verifies formatting, vet, unit/race tests, the native CLI capsule flow, PostgreSQL schema and runtime persistence, local Temporal propagation and capsule replay, Linux archives/checksums, and an unpacked CLI. The demo workflow checks exact faulty exit code 2 and a named contract violation plus clean exit 0; recordings are artifacts and do not rewrite source branches.
