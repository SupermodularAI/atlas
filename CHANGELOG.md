# Changelog

All notable changes to Atlas are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Atlas has not had a tagged release yet, so everything below is unreleased. Once
a version is tagged, `atlas.json`'s `schemaVersion` and the three `access`
values become a public contract that cannot change without a version bump — see
`docs/design.md` §5.

## [Unreleased]

### Added

- **`atlas` CLI.** Renders a company's published AI primitives into a browsable
  static site:

  ```bash
  atlas --descriptor company.yml --out ./site
  ```

  - `--descriptor` — path to the company descriptor (required)
  - `--out` — output directory (required)
  - `--strict` — exit non-zero if any source or package degraded, or any warning
    was recorded

- **`atlas check`** — the authoring-side gate for the publishing-side reader.
  Exits non-zero on any finding, so it can gate a merge request.

  ```bash
  atlas check [dir]            # lint primitive frontmatter under a tree
  atlas check --manifest FILE  # verify every pinned version resolves to a real tag
  ```

  Frontmatter linting catches two failure modes, and the second is why this is
  more than a parse check: an unquoted value containing `": "` is invalid YAML
  and the primitive is omitted, while an unquoted value containing `"#"` is
  *valid* YAML, silently truncated at the `#`, and gets listed wrongly with
  nothing reported anywhere.

  `--manifest` compares a marketplace manifest's pins against the tags that
  actually exist upstream, over `ls-remote` (no clone). It closes two silent
  drifts that are invisible from either repo alone: a package re-tagged upstream
  but not bumped in the manifest (Atlas keeps reading the old tag, successfully),
  and a version bumped in the manifest but never tagged upstream.

- **Two input kinds.** A descriptor may list published APM marketplaces and
  plain repositories carrying a `.claude/` tree. A company can have more than
  one marketplace, and `exclude` entries apply to both kinds.

- **`atlas.json`** — a machine-readable catalog emitting `schemaVersion: 2`. The
  `primitives` field distinguishes `null` ("not harvested") from `[]`
  ("harvested, genuinely empty"), and each package's `access` is exactly one of
  `public`, `restricted`, or `excluded`.

- **Self-contained `index.html`** — no external assets, no network at view time.
  All harvested text is escaped through `html/template`.

- **Two-level degradation.** A source that could not be reached
  (`status: unavailable`) and a package that could not be read
  (`access: restricted`) are reported distinctly and never collapsed into one
  state, so a reader can tell "we could not reach this" from "we reached it and
  were denied" (`docs/design.md` §7).

- **Withheld packages stay visible.** A package excluded by the descriptor is
  rendered as withheld rather than omitted, so the gap is auditable instead of
  silent.

- **Install commands derived from manifest fields.** A missing command is
  rendered as missing; Atlas never guesses one (`docs/design.md` §8).

- **Reproducible release builds** — `make release` cross-compiles with
  `-trimpath -buildvcs=false` and emits `SHA256SUMS`, so a consumer can pin a
  checksum rather than a mutable tag.

- **Repo-wide portability guard** — `internal/guard` fails the build if any
  company name, namespace, or package prefix is hardcoded in `internal/` or
  `cmd/`. Everything company-specific arrives via the descriptor or a fetched
  manifest.

- **Open-source project files** — `CONTRIBUTING.md`, `SECURITY.md`, and
  `CODE_OF_CONDUCT.md`.

### The claim boundary

Not a change, but the property most worth knowing about this tool, and one that
is deliberately not going to change (`docs/design.md` §9):

Atlas asserts that these primitives were published at these sources, at these
resolved SHAs, read at this timestamp, by a principal with this much access. It
does **not** assert that anything was approved, reviewed, unaltered, or
authorised to run. Atlas never classifies: it obeys a classification that
already exists and infers none.
