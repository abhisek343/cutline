# Development Workflow

## Working agreement

Every change starts from a behavioral claim and ends with reproducible evidence.
Avoid large horizontal scaffolding changes that cannot run end to end.

## Before coding

1. Select one roadmap acceptance criterion.
2. Read the relevant contracts and ADRs.
3. Inspect current code, tests, and open assumptions.
4. Write the target failure or passing fixture first when practical.
5. List normal, cancellation, timeout, duplicate, crash, and incomplete-evidence
   paths.
6. Define the exact evidence needed to judge the behavior.
7. Set the expected line-budget impact.

## Vertical-slice format

A slice should cross only the layers necessary to demonstrate one behavior:

```text
fixture -> SDK/adapter -> scheduler -> evidence -> contract -> CLI result
```

Example first slice:

- one checkout fixture;
- one `before-charge` point;
- one explicit context cancellation;
- one payment effect;
- one built-in invariant;
- one deterministic CLI failure.

Do not build generic minimization, HTML, or Temporal abstractions before this
slice works.

## Branch and commit discipline

- keep `main` releasable;
- use small topic branches;
- stage only files belonging to the slice;
- use terse imperative commit subjects;
- do not mix generated artifacts or local capsules into source commits;
- document schema and contract changes in the same commit.

## Validation ladder

Run the cheapest relevant check first:

1. focused package test;
2. affected fixture;
3. all unit tests;
4. race detector;
5. integration tests;
6. exact capsule replay;
7. full benchmark suite.

Record skipped checks honestly.

## Debugging discipline

When a test fails:

1. preserve the seed, campaign, and evidence;
2. classify target bug, harness bug, infrastructure failure, or flaky replay;
3. form one hypothesis;
4. add the smallest diagnostic evidence;
5. rerun the same schedule;
6. avoid unrelated rewrites;
7. remove temporary diagnostics or promote them into canonical evidence.

## Schema workflow

For a canonical schema change:

1. update model and validation;
2. add compatibility or rejection tests;
3. update database migration;
4. update JSON examples;
5. update CEL environment if exposed;
6. update capsule schema;
7. write or amend an ADR;
8. document migration impact.

Silent reinterpretation is forbidden.

## Definition of done

A slice is done when:

- acceptance criteria pass;
- relevant edge cases are tested;
- `go test -race ./...` passes when concurrency changed;
- fixture and CLI behavior were manually probed;
- generated evidence was inspected;
- documentation is current;
- no unrelated diff remains;
- line-budget impact is reported;
- remaining uncertainty is explicit.

## Review checklist

- Are cancellation boundaries named precisely?
- Can missing evidence produce a false pass?
- Is effect commitment authoritative?
- Are tasks and effects stably identified?
- Does replay preserve the same failure signature?
- Is ordering causal or merely timestamp-based?
- Is cleanup idempotent?
- Is target-controlled report content escaped?
- Is the new abstraction required by the current slice?
