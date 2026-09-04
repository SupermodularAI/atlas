# Contributing to Atlas

Thanks for your interest in Atlas. This document covers what you need to build it,
what the gates check, and the three project rules that are easiest to violate with
good intentions.

By taking part you agree to abide by our `CODE_OF_CONDUCT.md`. Notable changes go
in `CHANGELOG.md` under `## [Unreleased]`.

Read `docs/design.md` before changing behaviour. Several decisions in it are
load-bearing and covered by tests — a change that looks like a cleanup can quietly
break a guarantee. The design's numbered sections are treated as binding
constraints, and a review may cite them.

## Requirements

- Go 1.26 or newer
- `git` on `PATH` (Atlas resolves refs with `git ls-remote` and clones sources)

No other tooling is needed. Dependencies are **exact pins only**, limited to
`github.com/spf13/cobra` and `gopkg.in/yaml.v3`. Adding any other direct
dependency is a human decision — raise it in an issue first rather than opening a
PR that installs it. The standard library is sufficient for tests: please do not
add testify, ginkgo, or any assertion library.

## Build and test

```bash
go mod download

make lint      # go vet ./...
make test      # go test ./...
make build     # -> ./atlas
make example   # end-to-end: builds the committed fixture and renders a real atlas
make all       # lint + test + build
```

`make example` runs `./examples/run-example.sh`, which creates throwaway git repos
in a temp dir and renders a catalog from them. It needs **no network and no private
access** — if it fails on a clean checkout, that is a bug worth reporting.

## The two commands Atlas ships

Worth knowing before changing either, since they have different audiences:

```bash
atlas --descriptor company.yml --out ./site   # render a catalog
atlas check [dir]                             # lint primitive frontmatter
atlas check --manifest apm.yml                # verify manifest pins resolve to real tags
```

`atlas` renders; `atlas check` is the **authoring-side gate for the
publishing-side reader** — it runs in a package or marketplace repo's CI, not
in Atlas's. It exits non-zero on any finding so it can gate a merge request.
If you change what Atlas reads, check whether `check` needs to grow a
corresponding assertion: the two are meant to stay in step, and a reader that
tolerates something the gate doesn't flag is how silent defects reach a
catalog.

Formatting is plain `gofmt`:

```bash
gofmt -w .              # autofix
test -z "$(gofmt -l .)" # check
```

Before opening a PR, `make all && make example` should pass.

CI runs the same gates on every PR (`.github/workflows/ci.yml`), plus three
open-source gates (`.github/workflows/oss-gates.yml`): a secret scan over
history, `govulncheck` against dependencies, and a Conventional Commits check on
your commit subjects. Merge commits are exempt from that last one.

## Tests live next to the code

Unit tests are **colocated** and in the **same package** as their source, so
unexported functions are directly testable: `internal/render/page_test.go` sits
beside `internal/render/page.go`. Please follow that rather than introducing
`_test` packages.

Two conventions worth knowing before you write a test:

- **Tests that need a git repo build a real one.** See `NewFixtureRepo` in
  `internal/gitc/fixture_test.go`: it creates a repo in `t.TempDir()` and clones it
  over `file://`. Please do not replace this with mocks. The clone path, tag
  resolution, and the access-denied branch are the behaviours under test; a mock
  would assert nothing about git.
- **Repo-wide invariants go in `internal/guard/`**, which holds the checks that
  aren't tied to one package.

The CSS inside `internal/render/page.gohtml` is intentionally **not** unit-tested —
it is visual styling. The template's *escaping* and *content* are tested, and must
stay that way.

## Three rules that reviews enforce

These are the ones contributors trip over, so they're worth stating plainly.

### 1. No hardcoded organisation strings

`internal/` and `cmd/` must contain no company names, namespaces, package
prefixes, or usernames. Everything company-specific arrives via the descriptor or
a fetched manifest. This is enforced by a test
(`internal/guard.TestNoHardcodedOrgStrings`), and it caught two real defects in
test fixtures on its first run.

The distinction is between *structure* and *behaviour*: the Go module path is
unavoidably the repository URL, and that's exempt. A company name in a **value** —
a hardcoded `sourceBase`, a package-name prefix, a namespace — is the actual
defect, because it makes the tool work for one company only.

Use `example.test` / `example-co` in docs and fixtures, as the README does.

### 2. Atlas never classifies

Atlas may read a classification that already exists and obey it. It must never
infer that a primitive is sensitive, public, or approved. If something is withheld
from a catalog, that is because a human put it in an exclude list — and the page
still shows that a package was withheld, so the omission is visible rather than
silent.

### 3. Never widen a claim

This is the constraint most easily broken by well-intentioned copy. Per design §9:

> **Atlas asserts:** these primitives were published at these sources, at these
> resolved SHAs, read at this timestamp, by a principal with this much access.
>
> **Atlas does not assert:** that anything was approved, reviewed, unaltered, or
> authorised to run.

Output text may state what was published, at which SHA, and when it was read. It
may not state or imply that anything was approved, reviewed, unaltered, or
authorised to run. Approval state belongs to a governance control plane, not to a
catalog renderer.

A PR that adds a reassuring word like "verified", "approved", or "safe" to rendered
output will be asked to remove it, even where the surrounding change is good.

## Other conventions

- **Every exported symbol has a doc comment starting with its own name**, and
  `go vet ./...` must pass clean.
- **Wrap errors** with `fmt.Errorf("context: %w", err)`. Do not discard an error
  with `_` except where a deferred cleanup genuinely cannot be acted on.
- **YAML is read-only.** Atlas never marshals YAML to disk, because Go map ordering
  is nondeterministic. Atlas writes JSON and HTML only.
- **HTML uses `html/template`**, never `text/template`, and is never built by
  string concatenation. Escaping every interpolation is the injection guard for
  third-party frontmatter.
- **`nil` vs empty slice is semantic** in `internal/model`: `Primitives == nil`
  means "not harvested"; `Primitives == []` means "harvested, genuinely empty".
  Never normalise one into the other.
- **`atlas.json` is a public schema.** `schemaVersion`, the `primitives`
  null-vs-empty distinction, and the three `access` values cannot change without a
  version bump (design §5).

## Pull requests

- Base PRs on `main`.
- Branch naming: `<type>/<slug>`, where `<type>` is the Conventional Commit type of
  the primary change (`feat`, `fix`, `chore`, …).
- **Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/).**
  Subject ≤ 72 characters; body lines ≤ 100.
- **Your PR title must follow Conventional Commits too.** PRs are squashed, and
  the subject that lands on `main` is the PR title when the PR has more than one
  commit, or that commit's subject when it has exactly one. CI checks whichever
  one will actually be used, so a well-formed set of commits under a vague PR
  title still fails.
- Stage by explicit path. Please avoid `git add -A` / `git add .`.
- Keep commits small and reviewable — one logical change each.

A good PR description says what changed, why, and which gates you ran. If your
change touches behaviour described in `docs/design.md`, say which section and
whether the design still holds; if it supersedes a design decision, propose an ADR
under `docs/adr/` rather than editing the design doc in place.

## Reporting bugs

Open an issue with the Atlas version (`atlas --version`), your Go version, the
descriptor shape that triggered it (redacted — please don't paste private URLs),
and what you expected instead. If `make example` reproduces it on a clean
checkout, that's the most useful report there is.

For anything security-sensitive, see `SECURITY.md` instead of opening a public
issue.

## Local agent tooling

`.agent/` and `.claude/skills` hold a vendored SDLC pipeline used by the
maintainers' agent tooling. They are gitignored and are **not** part of Atlas: you
do not need them, and their absence from your clone is expected. If your editor or
agent harness looks for skills there, that's the reason.
