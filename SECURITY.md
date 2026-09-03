# Security Policy

## Reporting a vulnerability

**Please do not open a public issue for a security vulnerability.**

Report it privately through GitHub Security Advisories:

1. Go to the [Security tab](https://github.com/SupermodularAI/atlas/security/advisories)
   of this repository
2. Click **Report a vulnerability**

This creates a private thread visible only to you and the maintainers.

Please include the Atlas version (`atlas --version`), what an attacker could
achieve, and the smallest input that reproduces it — a descriptor shape or a
crafted manifest is usually enough. Redact private URLs and access tokens.

You should get an acknowledgement within 5 working days. If a report is
confirmed, we will agree a disclosure timeline with you before publishing.

## Supported versions

Atlas is pre-1.0. Fixes land on `main` and in the next tagged release; there are
no long-lived maintenance branches yet.

## What is in scope

Atlas reads third-party content — manifests, frontmatter, and repository trees it
does not control — and renders it into HTML. The interesting attack surface is
therefore anything that crosses that boundary:

- **HTML injection / XSS** in rendered output. Every interpolation of harvested
  text is escaped via `html/template`; a way to defeat that is a vulnerability.
- **Path traversal** while harvesting. A `sourceBase` or primitive path that
  escapes the intended tree is a defect (design §3 treats an escaping base as an
  error rather than a warning).
- **Argument injection** into git. Refs and URLs derived from a manifest reach
  `git`, so a value that smuggles a flag is in scope.
- **Disclosure of withheld content.** A package excluded by a descriptor must
  never have its contents harvested or rendered. Leaking any of it — including
  through `atlas.json` or a warning message — is a vulnerability.
- **Credential leakage** into `atlas.json`, rendered pages, or logs.

## What is out of scope

- **The absence of assurance claims is intentional, not a vulnerability.** Atlas
  asserts only that primitives were published at given sources and SHAs, read at
  a given time. It deliberately does *not* assert that anything was approved,
  reviewed, unaltered, or authorised to run (design §9). Reports that Atlas
  "fails to verify" approval, provenance, or safety describe the design.
- **Atlas trusting a classification it was given.** Atlas never classifies; it
  obeys a classification that already exists. A wrong exclude list is a
  misconfiguration in the descriptor, not a flaw in Atlas.
- Vulnerabilities in a source repository Atlas merely reads.
- Findings that require an attacker to already control the descriptor, since the
  descriptor is trusted input supplied by the operator.
