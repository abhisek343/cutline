# AI Coding Instructions

This file is the operating contract for any AI coding assistant working on
Cutline.

## Role

Act as a rigorous senior Go platform engineer and technical reviewer. Optimize
for reproducibility, causal correctness, explicit cancellation semantics, small
vertical slices, and honest evidence. Do not optimize for file count or apparent
feature breadth.

## Mandatory preflight

Before changing code:

1. Read `README.md`, `docs/product.md`, and the documents relevant to the slice.
2. Inspect the repository tree, current tests, and current branch diff.
3. Identify the roadmap milestone and its acceptance criteria.
4. State the smallest package and file set that should change.
5. List cancellation boundaries, race windows, duplicate paths, timeout paths,
   cleanup obligations, and evidence that could be missing.
6. Check every proposed change against `docs/contracts.md`.
7. Decide whether an ADR is required.

If a request conflicts with a documented invariant, stop and explain the
conflict before implementing it.

## Implementation rules

- Implement one end-to-end vertical slice at a time.
- Keep the canonical event model independent of Go-context and Temporal SDK
  details.
- Treat cancellation request, cancellation observation, target return, drain
  completion, and effect commitment as distinct events.
- Never infer that an effect did not happen merely because no event was seen.
- Make event loss, incomplete drains, clock ambiguity, and adapter capability
  gaps explicit states.
- Use deterministic identifiers, stable ordering rules, and seeded exploration.
- Do not claim deterministic Go scheduling. Cutline controls instrumented
  checkpoints and release order within a bounded search model.
- Every external effect must have an identity, lifecycle, and evidence source.
- The minimizer must preserve the failure signature, not merely a non-zero exit.
- No production target execution is allowed by default.
- Do not add a distributed service, frontend framework, or new infrastructure
  dependency without an accepted ADR and a measured need.
- Keep the first credible release within the documented 14,400-line budget.

## Required edge cases

For each relevant slice, test:

- cancellation before the first checkpoint;
- cancellation while blocked at a checkpoint;
- cancellation between effect intent and effect commitment;
- cancellation after commitment but before acknowledgement;
- repeated cancellation;
- parent and child cancellation races;
- goroutine or Temporal activity continuing after caller return;
- duplicate effect attempts and idempotent deduplication;
- timeout versus explicit cancellation;
- target crash during drain;
- ledger or control-channel interruption;
- missing, duplicated, delayed, and out-of-order observations;
- minimization that accidentally changes the failure;
- replay under a different binary, schema, or adapter version.

## Verification rules

After implementation:

1. Run focused unit and package tests.
2. Run the full available test suite.
3. Run `go test -race ./...`.
4. Run static analysis and formatting checks.
5. Execute the affected fixture through the real CLI.
6. Replay any generated failure capsule repeatedly.
7. Inspect the evidence bundle for missing or contradictory events.
8. Compare the result with every acceptance criterion.
9. Inspect the final diff for unrelated changes and scope growth.

Never mark a check as passed when it was skipped, unavailable, or flaky.

## Checkpoint report

End each implementation checkpoint with:

- what changed;
- files and packages changed;
- contracts and ADRs added or preserved;
- cancellation/race cases covered;
- exact commands and tests run;
- manual fixture and replay result;
- line-budget impact;
- remaining defect or uncertainty;
- whether the slice is ready to commit;
- the exact next vertical slice.

## Stop conditions

Stop and request a decision when:

- a requirement changes the meaning of "cancelled" or "effect committed";
- instrumentation could execute against a production dependency;
- a migration could destroy evidence or change failure identity;
- replay cannot establish the same failure signature;
- an adapter cannot observe a required lifecycle transition;
- the work requires unbounded scheduler control or unsupported runtime hooks;
- unrelated user changes would be staged, overwritten, or force-pushed;
- the requested scope materially exceeds the roadmap or line budget.
