# Documentation

Most users only need the README and a campaign file. The documents below are for contributors and for people who need to understand how Cutline reaches a verdict.

## Using Cutline

- [Contracts](contracts.md): writing CEL assertions and understanding pass, violation, and inconclusive results.
- [Failure capsules](failure-capsules.md): what Cutline writes for a failure and how replay works.
- [Failure model](failure-model.md): the classes of cancellation bugs the tool is designed to find.

## Building Cutline

- [Architecture](architecture.md): package layout and runtime boundaries.
- [System design](system-design.md): evidence flow, scheduling, and persistence.
- [Domain model](domain-model.md): the records carried through that flow.
- [Testing](testing-strategy.md): local and CI test layers.
- [Development workflow](development-workflow.md): day-to-day contributor workflow and checks.
- [Coding standards](coding-standards.md): conventions used in the codebase.

## Project planning

- [Implementation status](implementation-status.md)
- [Roadmap](roadmap.md)
- [Product notes](product.md)
- [Architecture decisions](adr/README.md)

The README is authoritative for supported commands and current scope. The code and tests are authoritative when documentation and implementation differ.
