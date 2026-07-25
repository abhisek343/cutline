# Architecture Decision Records

ADRs record decisions that materially constrain Cutline's design.

## Status values

- Proposed
- Accepted
- Superseded
- Rejected

## Accepted decisions

- [0001: Modular monolith with a small public SDK](0001-modular-monolith.md)
- [0002: Explicit checkpoints instead of full scheduler control](0002-explicit-checkpoints.md)
- [0003: CEL over a typed frozen evidence model](0003-cel-contracts.md)
- [0004: PostgreSQL as the authoritative evidence ledger](0004-postgresql-evidence-ledger.md)
- [0005: Versioned runtime adapter boundary](0005-versioned-runtime-adapters.md)

## Template

```markdown
# NNNN: Decision title

- Status: Proposed
- Date: YYYY-MM-DD

## Context

## Decision

## Consequences

### Positive

### Negative

## Alternatives considered

## Revisit when
```

ADRs are immutable after acceptance except for status and links. Supersede an
old decision with a new ADR rather than rewriting history.
