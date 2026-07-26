# Contributing to Cutline

Cutline accepts small, evidence-backed changes that preserve deterministic replay
and explicit cancellation semantics.

## Before opening a change

1. Read the [product definition](docs/product.md), [contracts](docs/contracts.md),
   and [development workflow](docs/development-workflow.md).
2. Choose one roadmap milestone or existing issue.
3. Describe the cancellation behavior or evidence gap being changed.
4. Identify whether the change requires an ADR.
5. Avoid unrelated cleanup in the same change.

## Change requirements

Every behavioral change must include:

- an explicit normal-case test;
- at least one cancellation or failure-path test;
- deterministic inputs or a recorded seed;
- a statement of the invariant preserved or introduced;
- replay evidence when the change touches scheduling, minimization, adapters, or
  failure capsules;
- documentation updates for public contracts.

Bug fixes should first add the smallest fixture that reproduces the bug.

## Development checks

The intended baseline commands are:

```text
go fmt ./...
go vet ./...
go test ./...
go test -race ./...
```

As tooling is added, `make check` will become the canonical aggregate command.
Integration tests that require PostgreSQL or Temporal must use pinned containers
and declare their prerequisites.

## Commit and pull-request style

- Keep commits coherent and reviewable.
- Use a concise imperative subject.
- Explain the failure mode and evidence in the pull-request body.
- Include exact commands run and any skipped checks.
- Do not include generated reports, secrets, credentials, or production traces.

## Architecture decisions

Add an ADR when a change:

- changes a canonical event or failure-capsule schema;
- changes cancellation or effect semantics;
- adds a runtime adapter or external infrastructure dependency;
- changes deterministic ordering or failure-signature rules;
- introduces a new process or service boundary;

Use the template in [docs/adr/README.md](docs/adr/README.md).

## Scope

Cutline is intentionally a compact engineering tool. Feature additions that do
not strengthen cancellation exploration, evidence, minimization, or replay are
likely out of scope for the first release.
