# Project Conventions — Atlas

Atlas renders a company's published AI primitives into a browsable static site. It is a
**reader**: it never classifies, builds, or publishes.

The full design is in `docs/design.md`. Read it before changing behaviour — several
decisions in it are load-bearing and tested.

## Stack & commands

| Purpose | Command |
| ------- | ------- |
| Install dependencies | `go mod download` |
| Run the app (dev) | `go run ./cmd/atlas --descriptor <path> --out <dir>` |
| **Verify gate** (lint + typecheck + unit tests) | `go vet ./... && go test ./...` |
| **E2E / integration gate** | `./examples/run-example.sh` |
| Autofix lint/format | `gofmt -w .` |
| Format check | `test -z "$(gofmt -l .)"` |
| Drive the running app (behavioral confirmation) | CLI: `go build -o atlas ./cmd/atlas` then run it against `examples/` and inspect `dist/example/index.html` |

The e2e gate (`./examples/run-example.sh`) does not exist until Task 11 creates it. Before
then, treat the e2e gate as not-yet-applicable and say so explicitly in the validation
report rather than reporting a false PASS.

## Apps & targets

- `single target: the atlas CLI (cmd/atlas, binary "atlas")`

## Testing strategy

- **Unit tests**: Go's built-in `testing` package, run by `go test ./...`. Tests are
  **colocated** with source: `internal/<pkg>/<name>_test.go` sits beside
  `internal/<pkg>/<name>.go`, in the same package (internal tests, not `_test` packages),
  so unexported functions are directly testable.
- **Fixtures**: tests that need a git repo build a real one in `t.TempDir()` and clone it
  over `file://` — see `internal/gitc/fixture_test.go`'s `NewFixtureRepo`. Do **not**
  replace this with mocks: the clone path, tag resolution, and the access-denied branch
  are the behaviours under test, and a mock would assert nothing about git.
- **Repo-wide invariants**: `internal/guard/` holds tests that are not tied to one
  package (e.g. the no-hardcoded-org-strings guard).
- **Integration / e2e**: `./examples/run-example.sh` — builds the committed fixture into
  throwaway git repos and renders a real atlas. No network, no private access.
- **Intentionally NOT unit-tested**: the CSS inside `internal/render/page.gohtml`
  (visual styling only). The template's *escaping* and *content* ARE tested; its colours
  and spacing are not.
- **No new test dependencies.** The standard library is sufficient. Do not add testify,
  ginkgo, or any assertion library.

## Coding conventions

- Language strictness: Go 1.26; `go vet ./...` must pass clean; every exported symbol has
  a doc comment starting with its own name.
- Error handling: wrap with `fmt.Errorf("context: %w", err)` — never discard an error with
  `_` except where a deferred cleanup genuinely cannot be acted on.
- Dependencies: **exact pins only**, no version ranges. Direct dependencies are limited to
  `github.com/spf13/cobra v1.10.2` and `gopkg.in/yaml.v3 v3.0.1`. Adding any other direct
  dependency requires a human decision — surface it, do not install it.
- YAML is **read-only**: never marshal YAML to disk (Go map ordering is nondeterministic).
  Atlas writes JSON and HTML only.
- HTML: use `html/template`, never `text/template`, and never build HTML by string
  concatenation. Escaping every interpolation is the injection guard for third-party
  frontmatter.
- **No hardcoded organisation strings** in `internal/` or `cmd/` — no company names,
  namespaces, package prefixes, or usernames. Everything company-specific arrives via the
  descriptor or a fetched manifest. Enforced by `internal/guard`.
- **Atlas never classifies.** It may read a classification that already exists and obey
  it; it must never infer that a primitive is sensitive, public, or approved.
- **Never widen a claim.** Output text may state what was published, at which SHA, read
  when. It may not state or imply that anything was approved, reviewed, unaltered, or
  authorised to run.
- `nil` vs empty slice is semantic in `internal/model`: `Primitives == nil` means "not
  harvested"; `Primitives == []` means "harvested, genuinely empty". Never normalise one
  into the other.

## Off-limits paths

- `.agent/skills/ — vendored SDLC pipeline skills, never hand-edited here`
- `.claude/skills — a symlink to .agent/skills, never replaced with a real directory`
- `docs/design.md — the approved design; propose changes in the report, do not edit it`

## Branch & commit policy

- **Integration branch** (PR base): `main`.
- **Feature branch naming**: `<type>/<TICKET>/<slug>` where `<type>` is the Conventional
  Commit type of the primary change (`feat`, `fix`, `chore`, …).
- **Base to cut from**: the integration branch (`main`).
- Commit format: Conventional Commits (see the `commit-message` skill). Subject ≤ 72
  chars; body lines ≤ 100. Commits are signed (`commit.gpgsign=true`, ssh format) — never
  pass `--no-gpg-sign`.
- Stage by **explicit path**. Never `git add -A` or `git add .`.

## Tooling / MCP

| Role | Configured? | Concrete tool |
| ---- | ----------- | ------------- |
| **Issue tracker + linked docs** (ingest) | no | None. Work is driven by `docs/design.md` and per-task plans seeded into `.agent/work/<TICKET>/`. |
| **Code forge** (push, PR, review threads) | no | None. Atlas has **no remote**: it is built locally and published later. Do not add a remote, push, or open a PR. |
| **Static analysis** (pr-review) | no | None beyond `go vet ./...`. |
| **Design source** (gather-context, implement-from-design) | no | None. Atlas has no UI design assets. |
| **Browser automation** (verify-changes behavioral gate, frontend) | no | None. Atlas emits a static file; confirm it by rendering and reading the HTML, not by driving a browser. |

Because the forge is **not configured**, the `pr-describe-draft` stage does not apply.
`pr-review` runs against the local diff (`git diff main...HEAD`) rather than a PR.

## Architecture decisions

| Role | Configured? | Location / tool |
| ---- | ----------- | --------------- |
| **ADRs / reference architecture** | yes | `docs/design.md` — the approved design doc. Treat its numbered sections as binding constraints. `docs/adr/` does not exist yet; create ADRs there if a decision supersedes the design. |

### Applicable design constraints

These are the sections `check-architecture` should resolve against, and the ones a review
may cite:

- **§3** — descriptor is the only input; excludes span both source kinds; repo mode fails
  closed on an unclassified repo.
- **§5** — `atlas.json` is a public schema: `schemaVersion`, `primitives` null-vs-empty,
  and the three `access` values cannot change without a version bump.
- **§7** — degradation has two distinct levels (`unavailable` source vs `locked`/
  `restricted` package) and they must never be collapsed.
- **§8** — install commands are derived from manifest fields; a missing command is
  correct, a guessed one is a defect.
- **§9** — the claim boundary. This is the constraint most easily violated by
  well-intentioned copy.
- **§10** — self-contained output, all harvested text escaped.

## Working directory

The pipeline writes per-ticket artifacts to `.agent/work/<TICKET-ID>/` — local working
files only, **never committed** (`.agent/work/` is gitignored; artifacts die with the
branch).
