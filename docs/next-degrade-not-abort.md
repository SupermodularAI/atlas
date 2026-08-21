# Next change: an unparseable primitive should degrade, not abort

## The problem, found on the first complete run against a real marketplace

Atlas aborted the entire atlas because one file in one package had invalid YAML
frontmatter:

```
atlas: harvest smos-dev-tooling: skills/glab/SKILL.md: parse frontmatter: invalid YAML
```

Seven other packages harvested fine and got no page at all. That is the same trade-off
`--strict` was deliberately kept out of the publishing job to avoid — *one unreadable
package should not mean no page, only a page that says a package was unreadable* — showing
up somewhere it was not anticipated.

## Why it is not simply a bug

`WalkTree` returning an error for an unnameable primitive was ATLAS-06's deliberate
fail-closed choice: **"a primitive Atlas cannot name is one it must not silently list."**
That reasoning is sound for a *disclosure* decision.

But §7's model governs *rendering*: degradation is recorded and made visible, never
silently dropped and never fatal. An unparseable SKILL.md is a data-quality problem in
someone's file, not a disclosure risk — the file's contents are never published either way.

So the two cases want opposite handling, and today they are the same code path.

## The trap: not every harvest error may be downgraded

**A symlink escape must stay fatal.** §3 made it an error on purpose:

> A repo containing `.claude -> /Users/someone/private` would otherwise cause Atlas to
> publish that private tree's primitives, because `os.Stat` and `os.ReadDir` follow
> symlinks. The operator reviewed the repo; they never reviewed the symlink target.

Downgrading that to a warning reopens the most serious defect found in this project's
development. A blanket "make harvest errors non-fatal" change would do exactly that.

**These are currently indistinguishable.** Both are plain `fmt.Errorf` values:

- `internal/harvest/walk.go:106,109` — `path %s escapes the walk root...`
- `internal/harvest/walk.go:~314` — `%s: parse frontmatter: ...`
- the undescribed-primitive error in `readDescribed`

`errors.Is` cannot separate them, so the sentinels come first.

## The change

**1. Add sentinels in `internal/harvest`.**

```go
// ErrEscapesRoot is a disclosure control failing closed: content outside the
// walk root was reachable. Never downgrade this to a warning.
var ErrEscapesRoot = errors.New("path escapes the walk root")

// ErrUnusablePrimitive is a data-quality problem in a harvested file —
// unparseable frontmatter, or a primitive with no description. The file's
// contents are never published either way, so this is safe to report rather
// than abort.
var ErrUnusablePrimitive = errors.New("primitive cannot be used")
```

Wrap the three existing error sites with the matching sentinel. Keep the existing
messages — the path and reason are what make them actionable.

**2. Split handling at both `build.go` call sites** (lines ~304 and ~371):

- `errors.Is(err, harvest.ErrEscapesRoot)` → **abort**, exactly as today.
- `errors.Is(err, harvest.ErrUnusablePrimitive)` → emit a `warnings[]` entry, mark the
  package, continue.
- anything else → abort. Unknown errors stay fatal; that is the fail-closed default and
  it is what stops a future error type being silently swallowed.

**3. Decide what the affected package renders as.** Two defensible options — pick one and
state the reasoning:

- **`access: "public"` with the unusable primitives omitted plus a warning.** Honest about
  what was harvested; risks a reader assuming the list is complete.
- **A fourth `access` value** (e.g. `"partial"`). More precise, but `atlas.json` is
  `schemaVersion: 1` and a new enum value is a schema change requiring a version bump.

The first is probably right, *provided* the warning names the file — the page must not imply
a complete listing when it is not. Whichever is chosen, `Primitives` must remain non-nil:
the package was harvested, so `null` would be a lie.

**4. New warning kind:** `unusable-primitive`, carrying the path and the reason. Add it to
`docs/design.md` §5 alongside `unused-exclude` and `duplicate-primitive`.

## Tests, and the one that matters most

- A repo with one unparseable SKILL.md and two good ones → page renders, two primitives
  listed, one `unusable-primitive` warning naming the file, exit 0.
- The same repo with `--strict` → non-zero exit. A warning is degradation.
- **A symlink escape still aborts.** Mutation-check this one specifically: make the escape
  path take the `ErrUnusablePrimitive` branch and confirm a test fails. That mutation is the
  whole safety property of this change.
- An unknown/unwrapped harvest error still aborts.

## Why this is worth doing rather than living with

The six frontmatter defects that triggered it are now fixed upstream, so the pipeline is
unblocked today. But Atlas reads *other people's* repos — that is its purpose — and it
cannot assume every primitive it meets is well-formed. A catalog tool that refuses to
render anything when one file is malformed is not usable against a real codebase it does
not control.
