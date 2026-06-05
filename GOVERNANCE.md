# Project Governance

This document describes how the Plorigo project is run today. It is intentionally lightweight
and will evolve as the community grows.

## Current model

Plorigo is in its early stage and is led by its founder and maintainer,
[@ismatBabirli](https://github.com/ismatBabirli), under the **Plorigo** organization.
Day-to-day decisions (roadmap, scope, merges, releases) are made by the maintainers, informed
by community discussion in issues and [Discussions](https://github.com/Plorigo/plorigo/discussions).

As the project matures we intend to:

- Grow a team of maintainers from active, trusted contributors.
- Move more decisions into the open (RFCs / design discussions for significant changes).
- Document a clear path from contributor → maintainer.

## Roles

- **Users** — anyone running or evaluating Plorigo. Feedback and bug reports are contributions.
- **Contributors** — anyone who opens an issue, PR, or helps in Discussions.
- **Maintainers** — trusted contributors with merge rights and review responsibility. Listed
  in [CODEOWNERS](./.github/CODEOWNERS). Maintainers review for correctness **and safety**
  (this tool runs on people's servers).
- **Lead maintainer** — currently the founder; breaks ties and owns the overall direction.

## How decisions are made

- **Small changes** (bug fixes, docs, well-scoped features): a maintainer review + green checks.
- **Significant changes** (architecture, security model, public API, license/packaging): discuss
  first in an issue or Discussion so the approach can be agreed before implementation.
- We favor **rough consensus** and keep discussion in the open whenever possible.

## Becoming a maintainer

There's no fixed checklist yet. Sustained, high-quality contributions — code, reviews, triage,
and helpfulness in Discussions — are what lead to an invitation. If you're interested, just keep
contributing and let us know.

## Code of Conduct

All participation is governed by our [Code of Conduct](./CODE_OF_CONDUCT.md).

## Changes to this document

Governance will be revisited as the project grows; changes are made via pull request.
