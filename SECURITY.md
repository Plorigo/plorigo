# Security Policy

Plorigo manages servers, containers, secrets, databases, backups, and deployments — the kind
of infrastructure where a vulnerability can have real consequences. We take security seriously
and deeply appreciate responsible disclosure.

## Supported versions

Plorigo is in **early development** and has not yet had a stable release. Until a `1.0`, only
the latest `main` and the most recent tagged pre-release receive security fixes.

| Version | Supported |
|---|---|
| `main` (latest) | ✅ |
| Tagged pre-releases | ✅ (latest only) |
| Older pre-releases | ❌ |

This table will be updated once stable releases begin.

## Reporting a vulnerability

**Please do not open a public issue, PR, or Discussion for security problems.**

Report privately using either of the following:

1. **GitHub Private Vulnerability Reporting (preferred).**
   Go to the repository's **Security** tab → **Report a vulnerability**, or open
   <https://github.com/Plorigo/plorigo/security/advisories/new>. This keeps the report
   private and lets us collaborate on a fix and a coordinated advisory.
2. **Email:** **hello@plorigo.com** — please include "SECURITY" in the subject line.

When reporting, please include as much as you can:

- A description of the vulnerability and its impact.
- Steps to reproduce or a proof of concept.
- Affected component(s) — e.g. control plane, agent, CLI, dashboard, secrets, backups.
- Version / commit and your environment (OS, Docker version, deployment method).
- Any suggested remediation.

## What to expect

- **Acknowledgement** within **3 business days**.
- An initial assessment and severity triage within **7 business days**.
- We'll keep you updated on progress and coordinate a disclosure timeline with you. Our goal is
  to ship a fix and publish an advisory promptly, typically within **90 days** of the report,
  sooner for actively exploited or high-severity issues.
- With your permission, we're happy to **credit you** in the advisory.

## Scope

Especially interested in reports affecting:

- The **server agent** and how it executes signed jobs / talks to the control plane.
- **Secrets** handling — encryption at rest, redaction, build-time vs runtime separation.
- **Privilege boundaries** — Docker socket exposure, privileged containers, host mounts, terminal access.
- **AuthZ/RBAC** — workspace/project isolation, production-deploy approvals, audit integrity.
- The **AI / MCP gateway** — ensuring agents cannot read raw secrets, delete production data, disable backups, or deploy to production without approval.

## Safe harbor

We will not pursue or support legal action against researchers who:

- Make a good-faith effort to follow this policy,
- Avoid privacy violations, data destruction, and service degradation, and
- Only interact with systems/accounts they own or have explicit permission to test.

If in doubt, ask us first at **hello@plorigo.com**. Thank you for helping keep Plorigo and
its users safe. 🔐
