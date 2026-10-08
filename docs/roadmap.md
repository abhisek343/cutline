# Roadmap

## Local first release

The supported workflow is explicit Go checkpoints, bounded exploration, native and command-launched local Temporal workers, optional PostgreSQL evidence persistence, CEL evaluation, minimization, verified capsules, replay, and static reports.

Completion requires the gates in [releasing](releasing.md) to pass on the exact reviewed commit. Code presence or a two-scenario demo is not release evidence.

## Maintained limits

- Scope is registered work; uninstrumented goroutines and dependency outcomes cannot be inferred.
- Canonical order is controlled evidence order, not arbitrary Go scheduling.
- Automatic counterexample minimization is supported for the documented effect predicate form; other valid CEL assertions can report violations without a minimizable witness.
- Temporal campaign cancellation uses the private native checkpoint protocol. The reference activity additionally verifies actual workflow cancellation propagation before attempting the business effect.
- An unresolved dependency, missing lifecycle transition, incomplete drain, unsupported history, or incompatible replay target is explicit rather than a pass.

## Deferred work

Remote or production execution, distributed scheduling, hosted reports, IDE integration, automatic instrumentation, and other workflow engines remain out of scope. More benchmark pairs and supported witness forms may be added after the local release gates remain green.
