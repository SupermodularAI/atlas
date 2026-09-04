# Atlas

Atlas turns a company's published AI primitives into a browsable static site.

A **primitive** is one reusable unit of AI tooling. Atlas recognises five kinds,
each by where it sits on disk — at the repo root or under `.claude/`:

| Primitive | Read from |
| --------- | --------- |
| `skill` | `skills/<name>/SKILL.md` |
| `subagent` | `agents/<name>.md` |
| `command` | `commands/<name>.md` |
| `hook` | `hooks/<file>` |
| `mcp_server` | `.mcp.json` |

That directory layout is the whole contract. **Any repo following it can be
harvested** — point Atlas at one with a `kind: repo` source and it lists what it
finds, no packaging tooling involved.

Repos can also be grouped into a **marketplace** and published as versioned
**packages**. That path is where [`apm`](https://github.com/microsoft/apm) — the
Agent Package Manager — comes in: Atlas expects a marketplace to carry an
`apm.yml` saying which package lives where at which version, and the install
commands it renders are `apm` commands. Atlas never runs `apm` and does not need
it installed; the only binary it executes is `git`.

Atlas is a **reader**. It never classifies, builds, or publishes.

## See it working

No network and no private access needed — the fixture builds throwaway git repos
in a temp dir:

```bash
make build
./examples/run-example.sh
open dist/example/index.html
```

That renders a catalog with one harvested package and one withheld one. See
`examples/README.md` for what each demonstrates.

## Install

Build from source. You need Go 1.26+ and `git` on your `PATH`:

```bash
git clone https://github.com/SupermodularAI/atlas.git
cd atlas
make build        # -> ./atlas
```

Usage examples below say `atlas`; a freshly built binary is `./atlas` until you
move it onto your `PATH`.

`make release VERSION=v0.1.0` cross-compiles binaries for linux/amd64 and
darwin/{arm64,amd64} plus a `SHA256SUMS` file. When a release is published, pin
the checksum rather than the tag: `git tag -f` moves a tag, a checksum cannot be
moved.

## Usage

Render a catalog:

```bash
atlas --descriptor company.yml --out ./site
```

| Flag | Meaning |
| ---- | ------- |
| `--descriptor` | path to the company descriptor (required) |
| `--out` | output directory (required) |
| `--strict` | exit non-zero if any source or package degraded, or any warning was recorded |

Atlas also ships an authoring-side gate, for the repos that *publish* primitives
rather than the one that reads them. Both exit non-zero on any finding, so they
can gate a merge request:

```bash
atlas check ./some-package        # lint primitive frontmatter
atlas check --manifest apm.yml    # verify every pinned version resolves to a real tag
```

`atlas check` catches two things. An unquoted frontmatter value containing `": "`
is invalid YAML, so the primitive is dropped. An unquoted value containing `"#"`
is *valid* YAML, silently truncated at the `#`, and gets listed wrongly with
nothing reported anywhere. `--manifest` catches a version pinned in a manifest
that was never tagged upstream.

## The descriptor

The only input. It lists a company's sources — a company can have more than one
marketplace:

```yaml
company: example-co
sources:
  - kind: marketplace          # a marketplace, with an apm.yml
    name: example
    url: https://git.example.test/example-co/marketplace
    exclude:                   # never harvested; rendered as withheld
      - pkg-confidential

  - kind: repo                 # any repo following the layout above
    name: some-service
    url: https://git.example.test/example-co/some-service
    acknowledgeUnclassified: true
```

Descriptors belong in the company's own repo, not here — a descriptor names
private URLs.

## What you get

Two files in `--out`, and nothing else. `index.html` is self-contained: no CDN,
no external assets, so it can be served from anywhere or opened from disk.
`atlas.json` is the same data as a stable, documented schema.

Each source carries a `status`:

| `status` | Meaning |
| -------- | ------- |
| `read` | Atlas resolved and read it |
| `unavailable` | Atlas could not reach it — recorded, not fatal |

Each package carries an `access`, and `summary.packages` counts them:

| `access` | Counted as | Meaning |
| -------- | ---------- | ------- |
| `public` | `harvested` | primitives were read and are listed |
| `restricted` | `restricted` | Atlas *could not* read it (no access), with the reason |
| `excluded` | `excluded` | Atlas *could* have read it but was told to withhold it |

`restricted` and `excluded` are deliberately different claims about what Atlas
knows, and Atlas never collapses them into one "missing" state. A withheld
package still appears as a card with the name and description its marketplace
publishes, so an omission is visible rather than silent.

Two things worth knowing when you read the output:

- **An empty `warnings[]` is a positive signal.** If an `exclude` pattern matched
  no real package, Atlas emits `unused-exclude` — and harvests the package. Empty
  means every pattern you wrote actually matched something.
- **Install commands are derived, never guessed.** They come from manifest fields;
  a package with nothing to derive from simply has no install command.

## What Atlas asserts

That these primitives were published at these sources, at these resolved commit
SHAs, read at this timestamp, by a principal with this much access.

**Atlas does not assert** that anything was approved, reviewed, unaltered, or
authorised to run. Approval state belongs to a governance control plane, not to a
catalog renderer.

## Documentation

- `docs/design.md` — the full design and the reasoning behind it
- `docs/first-run.md` — a real run against a private marketplace, including a
  limitation it uncovered
- `docs/ci-recipe.md` — running Atlas in CI, and why `url.insteadOf` auth does not
  work with it
- `examples/` — a runnable fixture needing no access to anything private
- `CONTRIBUTING.md` — how to build, test, and what reviews enforce
- `SECURITY.md` — how to report a vulnerability privately
- `CODE_OF_CONDUCT.md` — the standards expected of everyone taking part
- `CHANGELOG.md` — what has changed

Not part of Atlas, but relevant to the marketplace path:

- [`microsoft/apm`](https://github.com/microsoft/apm) — the Agent Package Manager
  that publishes the marketplaces and packages Atlas reads

## License

MIT
