# Testing

Cutline needs tests at several levels because a passing unit test does not show that a cancellation schedule is observable, stable, or replayable.

## Local checks

```sh
make check
make release
```

`make check` runs formatting, vet, unit tests, and the race detector. `make release` also checks the line budget, runs the reference replay benchmark, and builds the CLI.

## Integration checks

```sh
make integration
make temporal-integration
```

The PostgreSQL tests verify migrations, constraints, durable freezing, and idempotent migration application. The Temporal test starts local PostgreSQL and Temporal containers, runs and cancels a real workflow, and confirms that the adapter can fetch its history through the SDK.

## Fixtures

Every adapter change should have a faulty and corrected fixture where practical. A useful fixture has:

- a named checkpoint and a declared exploration bound;
- an authoritative effect outcome;
- one contract that fails for the faulty version and passes for the corrected one;
- a stable replay expectation when it produces a capsule.

The checkout fixture is the reference native example. The release test runs it repeatedly and expects at least 19 exact signatures from 20 faulty replays, while the corrected fixture must pass.

## What to cover

For changes involving cancellation or evidence, test the relevant timing boundary: before a checkpoint, while blocked, between effect attempt and commit, and after commit before acknowledgement. Also cover repeated cancellation, duplicate effects, target crashes during drain, missing or out-of-order observations, and minimization that changes a failure.

A missing observation must become inconclusive. It must never turn a failed safety assertion into a pass.
