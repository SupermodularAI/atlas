# Atlas — design

**Date:** 2026-08-18
**Status:** design decisions confirmed in conversation; spec pending review; not implemented
**Origin:** "we have a marketplace for supermodular-os on my personal playground —
can we use it to publish a catalog of primitives? How to do it for other codebases
and other companies?"

Atlas renders a company's published AI primitives into a static, browsable site.
It is a **reader**: it never classifies, builds, or publishes. It is a standalone
open-source project, not a stage in any existing pipeline.

---

## 1. Why this is a separate project

The obvious home was `os-dist/`, the emitter that already builds and publishes
supermodular-os as APM packages. That was rejected because **os-dist is being
deleted** — its job is moving into `apm migrate init` in an APM fork, gated on a
parity check (`30ed156 feat(os-dist): Phase 3 parity gate — PARITY OK`).

The lesson generalises into Atlas's central constraint:

> When a pipeline is in flux, depend on its **output format**, never its **file layout**.

Atlas therefore depends only on the *published* marketplace artifact — a format
that survives the emitter being rewritten in another language, moved to another
repo, or deleted. Nothing in the dependency graph is being deleted.

Rejected alternatives:

| Option | Why not |
|---|---|
| Stage inside `os-dist/` | The directory is being deleted. |
| Subcommand of `apm migrate` | Welds presentation into a packaging CLI; every page change needs a fork release. |
| Directory inside `manifest` | Couples Atlas's release to manifest's; declined in favour of OSS. |
| Reads `profiles.json` directly | Reads the *source* tree, so descriptions come from unscrubbed files and `confidential: true` primitives are in the input by construction. Also unusable for any other company. |

## 2. Scope

```
atlas --descriptor <path-or-url> --out <dir>
```

**Does:** read published marketplaces and/or plain repos, harvest primitive
metadata, merge with provenance, render a static site plus `atlas.json`.

**Does not:** classify primitives (that is `apm migrate init`'s job), build or
publish packages (APM's job), enforce policy (manifest's job), or store primitive
bytes.

### License and host support

- **MIT.** Chosen for minimum friction; note the packages Atlas reads are
  `UNLICENSED`, which is unrelated — Atlas's license covers Atlas.
- **Plain git, host-agnostic.** Atlas clones with whatever auth git already has
  (SSH agent, credential helper, `GIT_ASKPASS`). No GitLab-specific tokens, no
  host adapters, no assumptions about namespace depth.

This second point is a deliberate departure from supermodular-os's own pipeline,
which resolves packages via `sourceBase + '/' + source` — a workaround for
GitLab rejecting the default two-segment `source: <owner>/<repo>` form when repos
live four segments deep — formerly
`supermodularai/playgrounds/personal/joni.oliveira/...`, now
`supermodularai/core/transformation-stack/ai-primitives/...`, both four-deep (§14).
Atlas honours `sourceBase` when a manifest supplies it, but never requires it.

### No supermodular specifics in the code

`smos-` prefixes, `APM_CATALOG_DESCRIPTIONS`, and any concrete `sourceBase` must
never be constants in Atlas. Everything company-specific arrives via the
descriptor or the fetched manifest. **A test asserts no hardcoded organisation
strings appear in source.**

## 3. Input: the company descriptor

A company has **more than one marketplace**. The unit Atlas describes is therefore
a company, and its input is a descriptor listing sources.

```yaml
company: supermodular
sources:
  - kind: marketplace          # published APM marketplace
    name: ai-primitives
    url: https://gitlab.com/supermodularai/core/transformation-stack/ai-primitives/marketplace
    exclude:                   # packages never harvested, rendered as `excluded`
      - smos-finance
      - smos-access

  - kind: repo                 # plain repo, no published marketplace
    name: transformation-os
    url: https://gitlab.com/.../transformation-os
    exclude:                   # path globs; see "repo mode" below
      - "skills/finance-*/**"
    # acknowledgeUnclassified: true   # required if no classification file and no exclude
```

Two source kinds, one harvester:

- **`marketplace`** — resolve the published manifest, then harvest each package it
  names. Accurate about *what is actually shipped*.
- **`repo`** — skip manifest resolution; harvest the repo's `.claude/` tree
  directly. An inventory of *what exists*, classified or not.

`exclude:` applies to both kinds and means the same thing — *do not harvest this;
render it as `access: "excluded"` with the interior withheld.* It takes package
names for a `marketplace` source and path globs for a `repo` source. One concept,
one rendered treatment, two input shapes.

### Why `marketplace` mode needs excludes too

An earlier draft argued that marketplace sources are inherently safe because the
emitter excludes `confidential` primitives from what it publishes. **That is not
true of this marketplace.** The deliberate decision here was to publish
`smos-finance` and `smos-access` and gate them by **repo ACL** instead of
build-time exclusion (recorded in the emitter's own catalog descriptions:
"Governed by GitLab repo ACL, not build-time exclusion").

An ACL-based guarantee is only as narrow as the ACL. On 2026-08-18 these repos
moved into `supermodularai/core/transformation-stack/ai-primitives`, a group that
inherits ~35 members (~22 Developer+) from `transformation-stack`. **GitLab group
access is additive — project-level settings cannot narrow it.** So the clone that
was expected to fail now succeeds for those members, and a naive marketplace
harvest would render finance skill interiors.

This is the same failure mode as §"`repo` mode is not covered…" below, reached by a
different route: a safety property that came from outside Atlas, which then
changed. The fix is the same mechanism, so `exclude:` is defined once for both
kinds rather than twice.

Two consequences worth stating plainly:

- **Atlas cannot discover confidentiality from a published manifest.** The
  manifest carries `name`, `description`, `source`, and `version` — no audience,
  no `confidential` flag. So exclusion for marketplace sources *must* be declared
  in the descriptor; there is nothing for Atlas to infer from.
- **`excluded` is therefore an assertion by the descriptor author**, not a fact
  Atlas verified. §9's claim boundary applies: the atlas reports what it was told
  to withhold, not that the withholding was correct or complete.

`repo` mode requires no classification. Reading `name` and `description` from
`SKILL.md` frontmatter is the same operation `marketplace` mode performs on a
package's contents — it simply starts one stage later. Atlas invents no metadata
it cannot read from a file.

### `repo` mode is not covered by the marketplace safety argument

A `marketplace` source is inherently safe to render: the emitter that published it
already applied PII scrubbing, an audience ceiling, and unconditional exclusion of
primitives tagged `confidential`. Atlas has no code path to publish what was never
published.

**`kind: repo` has none of those properties.** It clones the raw tree, so it
enumerates whatever is on disk — including primitives a marketplace build would
have excluded, and frontmatter that was never scrubbed. Pointed at
supermodular-os, naive repo mode would enumerate `finance-ops`,
`externals-management`, and `finance-auditor`, plus hooks such as
`supermodular-os-telemetry.sh` (which hardcodes a live Notion token), and could
carry unscrubbed identifiers into `atlas.json` and the rendered page.

The safety property must therefore be re-established for this input kind rather
than inherited. Three mechanisms, all required:

1. **Honour a classification signal when one exists.** Before harvesting a repo,
   Atlas looks for a classification file (`profiles.json` shape: a
   `skills`/`agents` map whose entries may carry `confidential`, `audience`, and
   `piiScrub`). If found, any primitive marked `confidential`, or above the
   audience ceiling given by `--audience` (default: the most restrictive present),
   is emitted as `access: "excluded"` with `primitives` omitted — the same
   withheld-interior treatment a `locked` package gets. Atlas does not *classify*;
   it *obeys* a classification already written down.
2. **Descriptor-level excludes.** A repo source accepts `exclude:` globs, applied
   to primitive paths, for repos with no classification file.
3. **Fail closed on the unknown case.** A repo source with neither a
   classification file nor `exclude:` globs requires an explicit
   `acknowledgeUnclassified: true` in the descriptor. Without it Atlas refuses the
   source and says why. Rendering an unclassified repo is a decision the
   descriptor author must record, not a default.

This mirrors the posture the upstream emitter already takes — `writeApmCatalog`
throws rather than ship a package with no description, and
`assertAllSkillsClassified` fails the build on drift. Atlas fails closed for the
same reason: the unknown case is the dangerous one.

`--audience` has no effect on `marketplace` sources, whose filtering already
happened upstream.

### The walk root is a hard boundary

Every control above operates on paths **inside** the source, which makes the boundary itself a
disclosure guarantee — one that must be enforced rather than assumed. Atlas resolves symlinks and
harvests nothing outside the resolved root.

This is not hypothetical. A repo containing `.claude -> /Users/someone/private` would otherwise
cause Atlas to publish that private tree's primitives, because `os.Stat` and `os.ReadDir` follow
symlinks by default. The operator reviewed the repo; they never reviewed the symlink target, so no
exclude rule, classification file, or acknowledgement they wrote would apply to what got
published — every control is bypassed at once rather than one filter failing.

A symlink that stays within the root is legitimate and is followed normally. One that escapes
fails closed, and an escaping base (`.claude` itself pointing out of the tree) is an error rather
than a silent empty result: a repo that looks empty is indistinguishable from one that was never
read.

**Descriptors live in the company's own repo**, not in Atlas. Atlas is public;
bundling client namespace layouts into it would be a disclosure problem, and the
descriptor is arguably the client's own governance record. `--descriptor` accepts
a local path or a URL.

## 4. Pipeline

Four stages. Stage ② is the only one that can partially fail; ③ and ④ are pure
functions of their input.

```
 ① resolve    descriptor → per source:
                marketplace kind: fetch manifest → {name, sourceBase, owner,
                                   version, packages[]}
                repo kind:        synthesise a single implicit package
              → source unreachable ⇒ mark `unavailable`, continue
        ↓
 ② harvest    per package: git clone --depth 1 --filter=blob:none
              → at the tag from tagPattern if present, else default branch
              → record the resolved SHA either way
              → walk skills/, agents/, hooks/, commands/
              → parse YAML frontmatter → {type, name, description}
              → clone denied ⇒ mark package `locked`
        ↓
 ③ merge      union across sources, provenance stamped per entry
              → detect name collisions, annotate every side
        ↓
 ④ render     atlas.json → index.html (self-contained, no external requests)
```

### Why `atlas.json` sits between ③ and ④

The expensive half (clone + harvest, tens of seconds, network) is separated from
the cheap half (render, milliseconds, pure). Page design iterates against a cached
JSON file without re-cloning. The JSON is also handed to anyone who wants to build
their own view.

### Reproducibility

`tagPattern` is a field in *APM's* catalog schema and may be absent from a
hand-written descriptor or a `repo` source. Atlas therefore never *requires* a
tag: with no tag, it clones the default branch. In both cases it records the
resolved SHA in `atlas.json`. **Reproducibility comes from recording what was
fetched, not from requiring tags.**

## 5. `atlas.json` — a public schema

Atlas is public, so this file is a contract others build on. It carries
`schemaVersion` from the first release.

```json
{
  "schemaVersion": 1,
  "company": "supermodular",
  "generatedAt": "2026-08-18T11:20:00Z",
  "sources": [
    {
      "name": "ai-primitives",
      "kind": "marketplace",
      "status": "read",
      "sourceBase": "https://gitlab.com/supermodularai/playgrounds/personal/joni.oliveira",
      "owner": "supermodular",
      "version": "0.2.1"
    },
    { "name": "other", "kind": "marketplace", "status": "unavailable",
      "reason": "fetch failed: 404" }
  ],
  "packages": [
    {
      "name": "smos-infra",
      "source": "ai-primitives",
      "description": "Repo and workflow infrastructure…",
      "version": "0.2.1",
      "resolvedFrom": "https://gitlab.com/…/smos-infra",
      "resolvedSha": "99bbbb8d952b80882ce5a68fc588580f8f16756b",
      "access": "public",
      "primitives": [
        { "type": "skill", "name": "mr-review-agent",
          "description": "Automatically review GitLab MRs…" },
        { "type": "hook", "name": "slack_approval.py" }
      ],
      "install": {
        "marketplaceAdd": "apm marketplace add <url> --name ai-primitives",
        "install": "apm install smos-infra@ai-primitives --target claude"
      }
    },
    {
      "name": "smos-finance",
      "source": "ai-primitives",
      "description": "Confidential financial management…",
      "access": "excluded",
      "reason": "excluded by descriptor",
      "primitives": null
    }
  ],
  "collisions": [
    { "kind": "package-name", "name": "smos-infra", "sources": ["smos", "core"] }
  ],
  "warnings": [
    { "kind": "unused-exclude", "source": "other",
      "detail": "exclude pattern \"skils/*\" matched nothing" }
  ],
  "summary": { "sources": {"read": 1, "unavailable": 1},
               "packages": {"harvested": 6, "restricted": 2} }
}
```

Schema decisions that must not be ambiguous, since they cannot be changed without
a version bump:

- **`warnings[]` exists so a silently-ineffective control becomes visible.** It carries
  `{kind, source, detail}`. Two kinds are defined:

  - **`unused-exclude`** — an `exclude:` pattern that matched nothing during the run.
  - **`duplicate-primitive`** — the same `Type`+`Name` found at both the package root and its
    `.claude/` subtree. One entry is kept (root wins) and the duplicate is reported rather than
    silently doubled or silently dropped, following §6's "Atlas reports; a resolver decides".

  This field is in the schema from v1 deliberately. The exclude mechanism has now failed
  silently four times — repo mode enumerating raw trees, marketplace mode assuming the
  emitter had filtered, a malformed glob matching nothing, and a legal-but-inert pattern
  matching nothing. The first three were fixed as bugs; the fourth showed the shape is
  structural. **A pattern can be well-formed, accepted, and still withhold nothing**, and no
  amount of load-time validation can detect that — a typo'd `skils/*` is legal and matchable
  and simply wrong.

  So the mechanism stops being silent instead. This applies §7's existing principle — bounded
  coverage is stated in the output, never silently truncated — to excludes rather than
  packages. An operator who believes they withheld something and did not now learns it from
  the artifact.

- **`primitives: null` means "not harvested"** (`restricted` or `excluded`).
  **`primitives: []` means "harvested, genuinely empty."** These are distinct.
- **An `unavailable` source still appears in `sources`**, with `status` and
  `reason`. It is never silently absent. Its packages do not appear at all —
  Atlas does not know them.
- **`access`** is one of `"public"`, `"restricted"`, or `"excluded"`.
  `"restricted"` means Atlas could not read it (clone denied). `"excluded"` means
  Atlas *could* have read it but a classification signal or descriptor rule said
  not to render it (see §3). The distinction matters: one is a limit on the
  operator's access, the other is a deliberate withholding. Neither is a claim
  about the package's intended audience — only about what this run did.

### Relationship to manifest's schema (informational only)

Atlas uses an **independent schema** and does not depend on manifest. For anyone
composing the two later, the conceptual mapping is:

| Atlas | manifest |
|---|---|
| `packages[].primitives[]` | `skills` table rows |
| `sources[]` (`kind: repo`/`marketplace`) | `sources` table rows (`host`/`org`/`repo`/`label`) |
| `resolvedSha` | `content_hash` (different thing: commit vs. content digest) |
| — | `status`, `requested_by`, `decided_by` — Atlas has no approval concept |

Atlas has no equivalent of manifest's `status` lifecycle, and should not grow one:
approval state is manifest's job.

### Primitive types are a gap Atlas fills, not a convention it matches

Atlas defines a closed set: `skill | agent | hook | command | mcp`. manifest's
validator treats `primitive.type` as a **free-form string** — no closed enum
exists anywhere upstream. This set is therefore Atlas's own invention and should
be recognised as such if the two are ever reconciled.

## 6. Provenance and collisions

A package name is only meaningful **relative to its source**. In supermodular-os's
own marketplace, `source:` is a bare name (`smos-infra`, no slashes) resolved as
`sourceBase + '/' + source` — names were never globally unique. A union across
several marketplaces can therefore legitimately contain two `smos-infra`.

Consequences:

1. `sourceBase` must be carried alongside every package from stage ① onward, or
   stage ② holds a bare name it cannot turn into a URL.
2. Every rendered entry shows which source it came from. An unattributed union is
   useless as evidence.
3. Collisions are **reported, never resolved.** Both cards render, each labelled
   with its source, and the page carries a collision notice listing every clash.
   Atlas reports; a resolver decides.

Two collision kinds are detected:

- **`package-name`** — the same package name in two sources.
- **`primitive-name`** — the same skill/agent name in two different packages.
  More likely than the first, and more confusing to a consumer.

## 7. Degradation at two levels

Failure scope is visible on the page, and the two scopes are never collapsed:

| Scope | Marker | The page says |
|---|---|---|
| Source unreachable | `unavailable` | "source `<name>` could not be read — its packages are not listed" |
| Package clone denied | `locked` | "access restricted — primitives not listed" |

The distinction is real: an unreachable source means **unknown unknowns** (the
package count itself is unknown), whereas a locked package means **known
unknowns** (the manifest said it exists). Merging them would overstate what Atlas
knows.

### A third outcome: a configuration error is neither

Some failures are neither "could not read" nor "read successfully". A `tagPattern` resolving to a
tag that does not exist, or a `source` field Atlas cannot turn into a URL, means the **manifest is
wrong** — not that access was denied. Rendering those as `restricted` tells an operator they lack
permission when they do not, which is the §7 collapse in a third form.

These therefore **abort the run** with an error naming the package, the resolved ref, and the
`tagPattern` — rather than becoming a card or a `warnings[]` entry. The reasoning, decided during
implementation: a warning inside an otherwise-`exit 0` run is precisely the silently-ineffective
signal this design has been bitten by four times with excludes. An abort is what most reliably
gets the manifest fixed.

The distinction is enforced upstream in `internal/gitc`, which separates `ErrAccessDenied` (may
not read → locked card) from `ErrRefNotFound` (readable, ref missing → abort). That separation
cost two fix rounds to get right and must not be collapsed by a consumer.

A locked package still renders a card with its manifest-supplied name and
description, interior withheld. Three properties follow:

1. **Page shape does not vary by viewer** — the package list is manifest-derived,
   so two people with different access see the same cards, differently filled.
2. **No silent truncation** — bounded coverage is stated on the page, not just in
   logs.
3. It is the governance-correct answer: *"this exists and you may not see inside
   it."*

Build prints `2 sources: 1 read, 1 unavailable · 8 packages: 6 harvested, 2
restricted` and exits 0. `--strict` makes any degradation a non-zero exit, for CI
that must guarantee completeness.

## 8. Install commands

Rendered per package, derived from its own source's manifest — never hardcoded:

```
apm marketplace add <catalog-url> --name <source-name>
apm install <package>@<source-name> --target claude
```

The `@<source>` qualifier is load-bearing, not cosmetic: with collisions possible,
an unqualified `apm install smos-infra` is ambiguous. Rendering the qualified form
always is both correct and self-documenting.

These are the verified commands — supermodular-os's catalog README records them
working end-to-end against APM 0.26.0 (`apm install` integrated 8 skills + 1 hook
for `smos-infra`).

For `kind: repo` sources there is no install path, and none is rendered. **A
missing command is correct; a guessed one is a defect.** Likewise, if a
marketplace supplies no `sourceBase`, the APM command is omitted rather than
constructed.

A test asserts every rendered command is a pure function of manifest fields.

## 9. What Atlas claims, and what it must not

Atlas is read as evidence, so the strength of its claim matters.

**Atlas asserts:** *these primitives were published at these sources, at these
resolved SHAs, read at this timestamp, by a principal with this much access.*

**Atlas does not assert:** that anything was approved, reviewed, unaltered, or
authorised to run.

This restraint is required by facts upstream, not merely good taste:

- manifest's audit trail is **not tamper-evident**. Its own backlog (PROD-07)
  records: *"the substrate is missing, not just the verifier script… today an
  operator with DB write access can flip a decision undetectably."*
- Identity is **self-asserted** (`MANIFEST_PRINCIPAL` or `whoami`), a documented
  weakness.
- A marketplace is a discovery index with **no integrity guarantee** — sha256
  verification belongs to the APM *registry* transport, a different layer.

The rendered page therefore carries a generation stamp naming what was read and
when, following manifest's house convention for derived artifacts
(`renderApmPolicyYaml()` stamps `# Generated by manifest — DO NOT EDIT` with a
timestamp, per ADR-0004: derived, never hand-edited, regenerate-don't-patch).

**Note on scope overlap.** manifest's ADR-0019 forbids manifest holding primitive
*bytes* ("Bytes never transit manifest. No blob storage, no mirror, no publish
endpoint") but not primitive *metadata* — `sources` and `skills` are first-class
rows there. A metadata catalog is thus inside manifest's stated scope, so Atlas
being separate is a deliberate choice (OSS reach, independent cadence), not a
constraint. Atlas also deliberately avoids the noun "catalog" for its output,
because manifest already uses it in two distinct senses: the registry's
approved-rows set (frozen into the schema enum `denied_not_in_catalog`) and the
APM marketplace index. Atlas produces **an atlas**.

## 10. Rendering

Self-contained HTML — no external requests, no CDN, all CSS/JS inline — so a
generated atlas can be opened from disk, committed, or served from any static
host.

There is **no renderer to inherit from manifest**: its registry UI is a
Vite/React SPA behind a Bun API, not a static-site generator. The pattern worth
copying is `renderApmPolicyYaml()`'s: a zero-dependency emitter that derives
content fresh from its input, validates every value against an allow-charset
before emitting (defence against injection into the generated document), and
stamps a generated-by header.

That injection guard is not theoretical here: descriptions come from third-party
repo frontmatter and are rendered into HTML. **All harvested text is escaped on
output**, and a test asserts a frontmatter description containing `<script>` is
inert in the rendered page.

Structure: sources as top-level sections, package cards within, primitives listed
inside each card. Light and dark both supported via `prefers-color-scheme`.

## 11. Project layout

Conventions follow manifest's, which are proven and consistent:

- `apps/*` are deployable; `packages/*` are shared/portable.
- Colocated tests: `<name>.test.ts` beside `<name>.ts`.
- Exact dependency pins — no `^` or `~`.
- `docs/adr/NNNN-slug.md` (immutable; supersede, never edit), with an index
  flagging contested decisions. That convention exists because a real failure
  happened: an ADR renumbered 0017→0019 left stale citations in Notion.
- Specs at `docs/specs/YYYY-MM-DD-<topic>.md`; append-only `docs/NOTES.md`.

```
atlas/
  packages/core/          resolve · harvest · merge — portable, zero-dep
  packages/render/        atlas.json → HTML
  apps/cli/               the atlas binary
  examples/               fixture marketplace + descriptor (see below)
  docs/{adr,specs,NOTES.md}
  Makefile
  README.md  LICENSE (MIT)
```

### The demo problem

An OSS repo needs a runnable example, but the only real marketplace is private.
`examples/` therefore contains a **fixture marketplace committed to the repo**, so
`atlas --descriptor examples/demo.yml` works for a stranger with no access to
anything of ours. This is the same artifact the tests need — one fixture serving
both.

## 12. Testing

- **③/④ against fixture `atlas.json`** — no network. Covers collisions, both
  degradation levels, `primitives: null` vs `[]`, and HTML escaping.
- **① against fixture descriptors** — one source, several, one unreachable,
  malformed, and both `kind`s.
- **② against local git repos in a temp dir** — including one made unreadable, so
  `locked` is proven to trigger on a real clone failure rather than a mocked
  error.
- **End-to-end** on a two-source fixture with a deliberate package-name collision.

- **`repo`-mode disclosure fixtures** — a fixture repo carrying a classification
  file that marks one primitive `confidential` and one above the audience ceiling;
  a fixture repo with `exclude:` globs; and a fixture repo with neither.

Seven guarantee tests, the ones that must fail loudly if they regress:

1. A restricted package's primitive names never appear in output.
2. An unavailable source's packages never appear as if harvested.
3. No hardcoded organisation strings in source.
4. Harvested text containing markup is inert in the rendered page.
5. A `repo` source whose classification file marks a primitive `confidential`
   never emits that primitive's name — only an `access: "excluded"` card.
6. A `repo` source with neither a classification file nor `exclude:` globs and no
   `acknowledgeUnclassified: true` is **refused**, and the run exits non-zero.
7. `--audience company` against a `repo` source with a classification file omits
   every `personal`-tier primitive.

8. A `marketplace` source with `exclude: [smos-finance]` never emits any
   `smos-finance` primitive name — **even when the clone would have succeeded.**
   The fixture must grant access, so the test proves exclusion is enforced by the
   descriptor rather than incidentally by a failed clone.

Tests 5–8 are the ones that keep §3's re-established safety property true. They
are not covered by 1–2: those concern packages Atlas could not read, whereas these
concern primitives Atlas could read and must not render. Test 8 matters most,
because it is the case a passing test suite would otherwise flatter: if the fixture
denied access, the test would pass for the wrong reason and the exclusion logic
could be entirely absent.

## 13. Portability — the answer to "other codebases, other companies"

Reuse costs **writing a descriptor**. No code changes per company.

| Case | What is needed |
|---|---|
| Another codebase, same pipeline | A descriptor entry. Works because the input is the published format. |
| Another codebase, no APM at all | A descriptor entry with `kind: repo`. Inventory without classification. |
| Another company | Their own descriptor, in their own repo. Atlas unchanged. |
| A company wanting *classified* packages, not raw inventory | `apm migrate init` upstream. Atlas renders the result. |

The seam that makes this work: **classification is company-specific; rendering is
universal.** Atlas holds only the universal half, which is why it carries no
company knowledge.

This also gives `apm migrate init` a visible deliverable. Migration currently ends
with staged packages — correct but invisible. With Atlas downstream, it ends with
a page you can show a team.

### Honest limits

- **Clone-based harvest assumes git-backed packages.** A manifest pointing at
  tarballs needs a new fetcher. The fetcher is an interface; only the git
  implementation ships.
- **Atlas reflects what a given principal can read.** Two people generate
  differently-filled atlases from the same descriptor. Locked cards make this
  visible rather than silent, but the atlas is a view, not an absolute.
- **Atlas cannot detect what a descriptor omits.** An unlisted marketplace is
  invisible. The descriptor is a claim about completeness that Atlas takes on
  trust — which is exactly why it belongs in version control, where it can be
  reviewed.
- **`repo` mode's safety depends on a classification signal existing.** Atlas
  obeys one when present and refuses to proceed silently when absent (§3), but it
  cannot infer that an unmarked primitive is sensitive. For a repo with no
  classification, `acknowledgeUnclassified: true` moves that judgement to the
  descriptor author on the record. Running Atlas against a repo you have not
  reviewed, and publishing the result, remains an operator decision.

## 14. The first real target, as verified 2026-08-18

Atlas's day-one input, confirmed against live GitLab rather than assumed:

- **Marketplace:** `supermodularai/core/transformation-stack/ai-primitives/marketplace`
  (renamed from `smos-catalog`; the repos moved from
  `playgrounds/personal/joni.oliveira` on 2026-08-18).
- **9 private repos** in that group: 7 domain packages plus `smos-finance` and
  `smos-access`, and `marketplace` itself.
- **Tags resolve** at the new namespace (`v0.2.1`, `v0.2.0`, `v0.1.0` on
  `smos-infra`), so `tagPattern: "v{version}"` works.

**A latent failure Atlas will surface rather than cause.** The published manifest's
`sourceBase` still reads
`https://gitlab.com/supermodularai/playgrounds/personal/joni.oliveira` — the old
namespace. It resolves today only because **GitLab redirects the vacated path**:
`git ls-remote` against both old and new URLs returns the same SHA
(`99bbbb8d…`). That redirect dies the moment anything is created at the old path.

This is the emitter's hardcoded `APM_CATALOG_SOURCE_BASE`, not an Atlas defect —
Atlas reads `sourceBase` from the manifest, as designed. The relevant behaviour is
that recording `resolvedFrom` and `resolvedSha` (§5) makes the staleness **visible
in the artifact**: an atlas generated today truthfully shows resolution via the old
namespace, instead of hiding it behind a redirect that silently works. A renderer
would have shown nothing; a report shows the discrepancy.

## 15. Open items

- **Repo location.** GitHub, public, MIT — organisation account not yet chosen.
- **Descriptor schema versioning.** `atlas.json` carries `schemaVersion`; the
  descriptor format should too, before the first public release.
- **Whether Atlas ever reads manifest's approved rows** as a third source kind
  (`kind: manifest`). Deferred — it would make Atlas render approval state, which
  is a materially stronger claim than "this was published," and needs manifest's
  tamper-evidence gap (PROD-07) closed first to be honest.

### Accepted limitations

Verified, deliberately not fixed, recorded so nobody rediscovers them as surprises:

- **Unicode normalisation is not applied to primitive names.** Two primitives whose names are
  visually identical but differ in NFC vs NFD encoding (`café` as 6 bytes vs 5) are both harvested
  and are **not** reported as a duplicate, because the dedupe key is byte-exact. Confirmed by
  probe.

  Not fixed because: the disclosure consequence is nil — both primitives came from inside the walk
  root and both are legitimately harvested, so nothing leaks. The consequence is that a page could
  show two visually identical cards without flagging the collision, which is cosmetic rather than
  governance-affecting. A correct fix needs `golang.org/x/text/unicode/norm`, and this project
  limits direct dependencies to `cobra` and `yaml.v3` with any addition requiring a human decision.
  Adding a dependency to close a cosmetic gap is the wrong trade; revisit if a real marketplace
  ever hits it.

- **Shallow-clone flags are unexercised by the test suite.** `--depth 1` and
  `--filter=blob:none` are ignored by git for local `file://` clones, and the fixtures are local
  repos, so no test asserts shallow or partial-clone behaviour. Only real remote transport would.

- **Time-of-check/time-of-use on the walk-root boundary.** A symlink target could change between
  the boundary check and the read. Unaddressed: Atlas reads a tree it was pointed at by an operator,
  not a hostile filesystem racing it, and closing this would require opening by file descriptor
  throughout.
