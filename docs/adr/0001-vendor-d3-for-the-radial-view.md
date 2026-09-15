# ADR-0001 — Vendor D3 to render the radial view

**Date:** 2026-09-15
**Status:** accepted
**Supersedes:** part of design §10 ("no external requests, no CDN, all CSS/JS inline")

## Context

The page Atlas renders is a linear list: sections by source, cards by package,
`<details>` per primitive. It answers "what is in this package?" well and "how
big is this estate, and where is it concentrated?" not at all.

A radial mind map answers the second question at a glance. The prior art is
`supermodular.os-skilltree`, an interactive rotating radial tree of OS skills.

Two of skilltree's organising dimensions cannot be reproduced here, and this ADR
does not attempt them:

- **Category.** Skilltree hand-writes `category:` on every node. Atlas has no
  category field, and deriving one would be classifying — forbidden by §2 and
  assigned to `apm migrate init`.
- **Usage-weighted sizing.** Skilltree sizes dots by invocation count from a
  Notion analytics DB. §3 makes the descriptor the only input, and sizing by
  popularity implies endorsement, which §9 forbids.

What remains is the hierarchy Atlas already has: `source → package → primitive`.

## Decision

Render the radial view with **D3 v7.9.0, vendored into the repository** at
`internal/render/vendor/d3.v7.min.js` and inlined into every generated page.

Loading D3 from a CDN — as skilltree does — is not an option: §10's
self-containment is what lets an atlas be opened from disk, committed, or served
from a host with no outbound network. That part of §10 is untouched and remains
binding.

The part of §10 this supersedes is narrower: "all CSS/JS inline" previously
implied *our own* CSS/JS only. It now admits one vendored third-party library,
under the constraints below.

## Consequences

**Accepted cost.** D3 is 279,706 bytes. A generated page grows from ~13KB to
~302KB (measured on the committed fixture), so roughly 93% of every atlas is now
third-party JavaScript, and about 226KB of it (`d3-geo`, `d3-fetch`, `d3-force`,
`d3-array`, …) never executes.

This cost was raised explicitly. A three-module subset (`d3-hierarchy`,
`d3-shape`, `d3-path` — 47KB) was verified to produce identical radial output,
and the full library was chosen anyway, deliberately: one pinned artifact that
matches the upstream distribution is easier to audit, upgrade, and reason about
than a hand-picked module set whose transitive edges we would have to re-verify
on every bump.

**Integrity.** The vendored file is pinned by checksum, not by URL:

```
sha256  f2094bbf6141b359722c4fe454eb6c4b0f0e42cc10cc7af921fc158fceb86539
size    279706 bytes
source  https://cdn.jsdelivr.net/npm/d3@7.9.0/dist/d3.min.js
```

`internal/render` asserts both on every test run. A CDN URL is a mutable
pointer; the checksum is not. This is the same reasoning Atlas gives its own
consumers for pinning release binaries by SHA256 rather than by tag.

**Licence.** D3 is ISC (`internal/render/vendor/d3-LICENSE.txt`), which permits
redistribution provided the copyright notice appears in all copies. Every
generated atlas is a copy, so the upstream banner comment must survive into the
rendered page. A test asserts it does — stripping it would make each rendered
page a licence violation, which makes this a correctness constraint on the
template rather than a matter of tidiness.

**Not a Go dependency.** `go.mod` is unchanged and still pins exactly
`spf13/cobra` and `yaml.v3`. D3 is an embedded asset, not a module.

**Degradation stays visible (§7).** A tree layout naturally draws only nodes
that have children. `restricted` and `excluded` packages have no primitives
(`Primitives == nil`), so a naive port would silently omit exactly the packages
Atlas exists to make visible. They are therefore rendered as leaf nodes carrying
their own state, and the two levels stay distinguishable, as §7 requires. A test
asserts both appear.

**No claim is widened (§9).** Node size encodes primitive count — a fact Atlas
already computes and publishes in `atlas.json`. It does not encode usage,
quality, endorsement, or approval.

**JS-off behaviour.** The radial view requires JavaScript. The existing page
does not, and still does not: the view is additive, rendered into a container
that is empty (and hidden) without JS, so every primitive remains readable in
the cards below it. This preserves the promise the existing inline script
already makes.

## Alternatives rejected

| Option | Why not |
| ------ | ------- |
| D3 from CDN | Breaks §10 self-containment: an atlas would stop rendering offline, from disk, or behind an egress firewall. |
| D3 subset (47KB) | Verified equivalent and 17% of the size; rejected in favour of one auditable upstream artifact. |
| Hand-rolled polar math | No dependency and §10 untouched, but ~150 lines of layout code to own and test, for a worse result than a maintained library. |
| No visualisation | The status quo. Rejected: the linear page genuinely does not answer the scale question. |
