# Failure capsules

A failure capsule is a portable directory for one minimized, stable violation. It gives another developer enough information to replay the failure without reading raw logs or reconstructing the original schedule by hand.

## Contents

A capsule includes:

- a manifest with schema, tool, target, and adapter versions;
- the normalized campaign and deterministic seed;
- the minimized cancellation and release schedule;
- ordered canonical events and the derived task/effect view;
- the violated contract and stable failure signature;
- checksums and redacted dependency metadata.

Capsules do not capture environment variables or arbitrary payloads by default.

## Creating one

```sh
cutline minimize --campaign cutline.yaml --output capsules/checkout-cancel
```

Minimization keeps a candidate only when it reproduces the same failure signature within the configured confirmation budget. A different error or a different contract violation is not a successful reduction.

## Replaying and reporting

```sh
cutline replay capsules/checkout-cancel
cutline report capsules/checkout-cancel --format html --output report.html
```

Replay reports whether the signature is exact, different, not reproduced, or incompatible with the local target. Reporting reads validated capsule data only and does not execute the target.

## Safety and compatibility

Capsule import validates checksums, schema versions, paths, and symlinks before reading executable metadata. Invalid or tampered capsules are rejected. A target or adapter version mismatch is reported explicitly; Cutline does not silently reinterpret a previous result.

## Execution provenance

New capsules include `execution.json`, tying the minimized schedule, frozen run/attempt identity, and failure signature to the final confirmed execution. Packaging reruns the final minimized schedule; it does not attach the original larger run's events to a smaller schedule.

The target identity is the content hash of its compiled Linux executable, plus its adapter version. Go test targets are compiled before hashing. Replay rebuilds/checks this identity before execution; a changed binary or unsupported legacy signature is rejected. Intentional target changes require explicit `--comparative` and are labelled comparative. Interpreters/scripts and unsupported opaque command forms are refused for capsule provenance rather than given an unverifiable compatibility promise.

These checks cover the recorded executable, supported build arguments and adapter compatibility. They are not a snapshot of arbitrary external services, environment variables, or production credentials. Keep replay dependencies local and controlled.
