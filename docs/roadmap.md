# Roadmap

The native Go path, evidence ledger, contracts, exploration, minimization, capsules, reports, and local Temporal history ingestion are implemented.

## Next work

1. Add a Temporal worker instrumentation protocol.
2. Run checkpoint-driven Temporal campaigns against local workers.
3. Add faulty and corrected Temporal fixtures with stable capsule replay.
4. Add more native benchmark pairs and publish search bounds.
5. Prepare installation, compatibility, and release documentation for a pre-release tag.

The Temporal work is intentionally local-first. It must preserve the existing rules: external effect outcomes remain explicit, evidence gaps are inconclusive, and replay preserves the failure signature.

## Deferred work

Remote or production execution, distributed scheduling, hosted reports, IDE integration, automatic instrumentation, and other workflow engines remain out of scope.
