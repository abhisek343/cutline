# Security Policy

Cutline executes test targets and records detailed behavioral evidence. Treat it
as privileged developer tooling.

## Supported versions

No production release exists yet. Security fixes apply to the current `main`
branch until a version-support policy is published.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Contact the repository
owner privately with:

- affected commit or version;
- reproduction steps;
- expected and observed impact;
- whether credentials, production systems, or third-party data were involved;
- any suggested mitigation.

## Trust boundaries

- Campaign files and target commands are trusted input. Running an untrusted
  campaign is equivalent to running untrusted local code.
- Target services and Temporal workflows may be buggy or hostile to the control
  channel.
- PostgreSQL evidence is not a secret store.
- Failure capsules may contain business identifiers, payload fragments, paths,
  environment metadata, and protocol messages.
- Static HTML reports must escape all target-controlled content.

## Safe defaults

Cutline must:

- refuse obvious production endpoints unless explicitly overridden;
- bind local control interfaces to loopback or a private Unix socket;
- redact configured secrets before persistence;
- exclude environment variables by default;
- cap event, payload, and report sizes;
- validate capsule paths and archive entries;
- avoid shell interpolation when executing target commands;
- keep active-content scripts out of imported report data;
- record tool and schema versions for untrusted capsule handling.

## Not a sandbox

Cutline does not sandbox arbitrary code. Use disposable containers, test
credentials, and isolated dependencies. Never point a campaign at real payment,
email, messaging, or customer-data systems.

## Sensitive-data removal

If sensitive data is committed or included in a capsule, rotate the affected
credential first. Deleting a branch or rewriting Git history does not guarantee
immediate removal from caches, forks, clones, or hosting-provider retention.
