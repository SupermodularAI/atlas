# Atlas Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go CLI that reads a company descriptor, harvests primitive metadata from published APM marketplaces and plain repos, and emits `atlas.json` plus a self-contained static HTML site.

**Architecture:** Four sequential stages, each an `internal/` package with an explicit exported surface: `resolve` (descriptor → sources → packages), `harvest` (clone → frontmatter → primitives), `merge` (union + provenance + collisions), `render` (model → HTML). `atlas.json` sits between merge and render so the expensive network half is decoupled from the pure rendering half. Cloning shells out to the `git` binary so the user's existing auth applies unchanged.

**Tech Stack:** Go 1.26, cobra v1.10.2, gopkg.in/yaml.v3 v3.0.1 (read only), `html/template` (stdlib, escapes by default), `go test` with `t.TempDir()` git fixtures.

**Spec:** `docs/superpowers/specs/2026-08-18-atlas-design.md` (in supermodular-os, branch `worktree-feat+primitives-catalog-pages`). **Copy the spec into the Atlas repo at `docs/design.md` in Task 1** — the plan argues from it and executors need both.

## Global Constraints

- **Location:** build in `~/workspace/atlas` (a fresh git repo, not a worktree of anything). GitHub publication is deferred; do not add remotes.
- **Go 1.26**, module path `github.com/supermodular/atlas` (placeholder — the org is an open item in spec §15; do not create the remote).
- **Exact dependency pins, no `^`/`~` semantics.** `cobra v1.10.2`, `yaml.v3 v3.0.1`. No other direct dependencies without an explicit decision.
- **No hardcoded organisation strings** anywhere in `internal/` or `cmd/` — no `smos-`, no `supermodularai`, no concrete `sourceBase`. Everything company-specific arrives via descriptor or fetched manifest. Enforced by a test (Task 11).
- **`yaml.v3` is read-only.** Never marshal YAML to disk (Go map ordering is nondeterministic). Atlas writes JSON and HTML only.
- **All harvested third-party text is escaped on output.** Use `html/template`, never `text/template`, never string concatenation into HTML.
- **Atlas never classifies.** It reads classification when present and obeys it; it never infers.
- **Layout:** `cmd/atlas/` for the entrypoint, `internal/<stage>/` for implementation — matching `flux-capacitor` and `flux`.
- **Claim boundary (spec §9):** Atlas asserts what was published, at which SHA, read when. It never asserts approval, review, or integrity. No output text may imply otherwise.

---

### Task 1: Repo skeleton, module, and the design doc

**Files:**
- Create: `~/workspace/atlas/go.mod`
- Create: `~/workspace/atlas/LICENSE` (MIT)
- Create: `~/workspace/atlas/README.md`
- Create: `~/workspace/atlas/.gitignore`
- Create: `~/workspace/atlas/docs/design.md` (copy of the spec)
- Create: `~/workspace/atlas/Makefile`

**Interfaces:**
- Consumes: nothing.
- Produces: a Go module that builds; `make test` and `make build` targets later tasks rely on.

- [ ] **Step 1: Create the repo and module**

```bash
mkdir -p ~/workspace/atlas && cd ~/workspace/atlas
git init
go mod init github.com/supermodular/atlas
```

- [ ] **Step 2: Set the repo-local commit identity**

The `ai-primitives` group rejects non-`@supermodular.ai` committer emails, and the
global git identity is a gmail address. Atlas has no remote yet, but set this now so
the first push anywhere does not fail:

```bash
cd ~/workspace/atlas
git config user.email "joni.oliveira@supermodular.ai"
git config user.name "Jóni Oliveira"
```

- [ ] **Step 3: Write the MIT LICENSE**

```
MIT License

Copyright (c) 2026 Supermodular

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

- [ ] **Step 4: Write `.gitignore`**

```
/atlas
/dist/
*.test
```

- [ ] **Step 5: Copy the design doc in**

```bash
cp /Users/jonioliveira/workspace/supermodular-os/.claude/worktrees/feat+primitives-catalog-pages/docs/superpowers/specs/2026-08-18-atlas-design.md \
   ~/workspace/atlas/docs/design.md
```

- [ ] **Step 6: Write the Makefile**

```makefile
.PHONY: test build lint all
all: lint test build

test:
	go test ./...

build:
	go build -o atlas ./cmd/atlas

lint:
	go vet ./...
```

- [ ] **Step 7: Write README.md**

```markdown
# Atlas

Renders a company's published AI primitives into a browsable static site.

Atlas is a **reader**. It never classifies, builds, or publishes: it resolves
published APM marketplaces (or plain repos), harvests primitive metadata, and
emits `atlas.json` plus a self-contained `index.html`.

## What Atlas asserts

That these primitives were published at these sources, at these resolved commit
SHAs, read at this timestamp, by a principal with this much access.

**Atlas does not assert** that anything was approved, reviewed, unaltered, or
authorised to run. Approval state belongs to a governance control plane, not to
a catalog renderer.

## Usage

```bash
atlas --descriptor company.yml --out ./site
```

See `docs/design.md` for the full design, and `examples/` for a runnable
fixture that needs no access to anything private.

## License

MIT
```

- [ ] **Step 8: Verify it builds and commit**

```bash
cd ~/workspace/atlas
go build ./... && go vet ./...
git add -A
git commit -m "chore: Atlas repo skeleton, MIT license, design doc"
```

Expected: build and vet both succeed with no packages yet (no output).

---

### Task 2: Descriptor parsing

**Files:**
- Create: `~/workspace/atlas/internal/descriptor/descriptor.go`
- Test: `~/workspace/atlas/internal/descriptor/descriptor_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Source struct { Kind, Name, URL string; Exclude []string; AcknowledgeUnclassified bool }`
  - `type Descriptor struct { Company string; Sources []Source }`
  - `func Load(path string) (*Descriptor, error)`
  - `func (s Source) IsExcluded(name string) bool` — exact-match for marketplace kind, glob for repo kind

- [ ] **Step 1: Write the failing tests**

```go
package descriptor

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "d.yml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMarketplaceSource(t *testing.T) {
	d, err := Load(write(t, `
company: acme
sources:
  - kind: marketplace
    name: mkt
    url: https://example.test/mkt
    exclude:
      - pkg-secret
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if d.Company != "acme" {
		t.Errorf("Company = %q, want acme", d.Company)
	}
	if len(d.Sources) != 1 {
		t.Fatalf("len(Sources) = %d, want 1", len(d.Sources))
	}
	s := d.Sources[0]
	if s.Kind != "marketplace" || s.Name != "mkt" {
		t.Errorf("got Kind=%q Name=%q", s.Kind, s.Name)
	}
	if !s.IsExcluded("pkg-secret") {
		t.Error("pkg-secret should be excluded")
	}
	if s.IsExcluded("pkg-open") {
		t.Error("pkg-open should not be excluded")
	}
}

func TestRepoSourceGlobExclude(t *testing.T) {
	d, err := Load(write(t, `
company: acme
sources:
  - kind: repo
    name: r
    url: https://example.test/r
    exclude:
      - "skills/finance-*/**"
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := d.Sources[0]
	if !s.IsExcluded("skills/finance-ops/SKILL.md") {
		t.Error("finance-ops path should be excluded")
	}
	if s.IsExcluded("skills/code-review/SKILL.md") {
		t.Error("code-review path should not be excluded")
	}
}

func TestRejectsUnknownKind(t *testing.T) {
	_, err := Load(write(t, `
company: acme
sources:
  - kind: wormhole
    name: x
    url: https://example.test/x
`))
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestRejectsMissingCompany(t *testing.T) {
	_, err := Load(write(t, `
sources:
  - kind: repo
    name: x
    url: https://example.test/x
    acknowledgeUnclassified: true
`))
	if err == nil {
		t.Fatal("expected error for missing company")
	}
}

func TestRejectsDuplicateSourceNames(t *testing.T) {
	_, err := Load(write(t, `
company: acme
sources:
  - kind: repo
    name: dup
    url: https://example.test/a
    acknowledgeUnclassified: true
  - kind: repo
    name: dup
    url: https://example.test/b
    acknowledgeUnclassified: true
`))
	if err == nil {
		t.Fatal("expected error for duplicate source names")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd ~/workspace/atlas && go test ./internal/descriptor/ -v`
Expected: FAIL — `undefined: Load`.

- [ ] **Step 3: Add the yaml dependency**

```bash
cd ~/workspace/atlas
go get gopkg.in/yaml.v3@v3.0.1
```

- [ ] **Step 4: Write the implementation**

```go
// Package descriptor loads and validates the company descriptor — Atlas's only
// input. A company has more than one marketplace, so the descriptor, not a URL,
// is the unit Atlas describes.
package descriptor

import (
	"fmt"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// Source kinds.
const (
	KindMarketplace = "marketplace"
	KindRepo        = "repo"
)

// Source is one place Atlas reads primitives from.
type Source struct {
	Kind string `yaml:"kind"`
	Name string `yaml:"name"`
	URL  string `yaml:"url"`

	// Exclude lists things never harvested, rendered as access "excluded".
	// Package names for KindMarketplace; path globs for KindRepo.
	Exclude []string `yaml:"exclude"`

	// AcknowledgeUnclassified permits a repo source that carries neither a
	// classification file nor excludes. Atlas fails closed without it.
	AcknowledgeUnclassified bool `yaml:"acknowledgeUnclassified"`
}

// Descriptor is the parsed company descriptor.
type Descriptor struct {
	Company string   `yaml:"company"`
	Sources []Source `yaml:"sources"`
}

// IsExcluded reports whether name is excluded by this source. Marketplace
// sources exclude by exact package name; repo sources by path glob.
func (s Source) IsExcluded(name string) bool {
	for _, pat := range s.Exclude {
		if s.Kind == KindMarketplace {
			if pat == name {
				return true
			}
			continue
		}
		if matchGlob(pat, name) {
			return true
		}
	}
	return false
}

// matchGlob supports the "**" suffix that path.Match does not, so a pattern
// like "skills/finance-*/**" matches at any depth beneath the prefix.
func matchGlob(pattern, name string) bool {
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		// Match the prefix itself against each ancestor path of name.
		for p := name; p != "." && p != "/" && p != ""; p = path.Dir(p) {
			if ok, _ := path.Match(prefix, p); ok {
				return true
			}
		}
		return false
	}
	ok, _ := path.Match(pattern, name)
	return ok
}

// Load reads, parses and validates a descriptor file.
func Load(p string) (*Descriptor, error) {
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read descriptor: %w", err)
	}
	var d Descriptor
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("parse descriptor: %w", err)
	}
	if err := d.validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

func (d *Descriptor) validate() error {
	if strings.TrimSpace(d.Company) == "" {
		return fmt.Errorf("descriptor: company is required")
	}
	if len(d.Sources) == 0 {
		return fmt.Errorf("descriptor: at least one source is required")
	}
	seen := map[string]bool{}
	for i, s := range d.Sources {
		switch s.Kind {
		case KindMarketplace, KindRepo:
		default:
			return fmt.Errorf("descriptor: sources[%d]: unknown kind %q (want %q or %q)",
				i, s.Kind, KindMarketplace, KindRepo)
		}
		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("descriptor: sources[%d]: name is required", i)
		}
		if strings.TrimSpace(s.URL) == "" {
			return fmt.Errorf("descriptor: sources[%d]: url is required", i)
		}
		if seen[s.Name] {
			return fmt.Errorf("descriptor: duplicate source name %q — names must be unique, they qualify install commands", s.Name)
		}
		seen[s.Name] = true
	}
	return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd ~/workspace/atlas && go test ./internal/descriptor/ -v`
Expected: PASS — all five tests.

- [ ] **Step 6: Commit**

```bash
cd ~/workspace/atlas
git add go.mod go.sum internal/descriptor/
git commit -m "feat(descriptor): load and validate the company descriptor

Excludes span both source kinds with one meaning — do not harvest, render as
excluded — taking package names for marketplace sources and path globs for repo
sources. KnownFields(true) so a typo'd field is an error, not silence."
```

---

### Task 3: The `atlas.json` model

**Files:**
- Create: `~/workspace/atlas/internal/model/model.go`
- Test: `~/workspace/atlas/internal/model/model_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: the public schema types every later stage uses.
  - `const SchemaVersion = 1`
  - Access constants: `AccessPublic`, `AccessRestricted`, `AccessExcluded` = `"public"`, `"restricted"`, `"excluded"`
  - Status constants: `StatusRead`, `StatusUnavailable` = `"read"`, `"unavailable"`
  - Primitive type constants: `TypeSkill`, `TypeAgent`, `TypeHook`, `TypeCommand`, `TypeMCP`
  - `type Primitive struct { Type, Name, Description string }`
  - `type Install struct { MarketplaceAdd, Install string }`
  - `type Package struct { Name, Source, Description, Version, ResolvedFrom, ResolvedSha, Access, Reason string; Primitives []Primitive; Install *Install }`
  - `type Source struct { Name, Kind, Status, SourceBase, Owner, Version, Reason string }`
  - `type Collision struct { Kind, Name string; Sources []string }`
  - `type Summary struct { Sources map[string]int; Packages map[string]int }`
  - `type Atlas struct { SchemaVersion int; Company, GeneratedAt string; Sources []Source; Packages []Package; Collisions []Collision; Summary Summary }`
  - `func (a *Atlas) MarshalJSONIndent() ([]byte, error)`

- [ ] **Step 1: Write the failing tests**

The critical case is `primitives: null` vs `[]` — spec §5 makes these distinct and
a version bump would be needed to change it.

```go
package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrimitivesNullVsEmptyAreDistinct(t *testing.T) {
	withheld := Package{Name: "a", Access: AccessExcluded, Primitives: nil}
	empty := Package{Name: "b", Access: AccessPublic, Primitives: []Primitive{}}

	wb, err := json.Marshal(withheld)
	if err != nil {
		t.Fatal(err)
	}
	eb, err := json.Marshal(empty)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wb), `"primitives":null`) {
		t.Errorf("withheld package must emit primitives:null, got %s", wb)
	}
	if !strings.Contains(string(eb), `"primitives":[]`) {
		t.Errorf("harvested-empty package must emit primitives:[], got %s", eb)
	}
}

func TestSchemaVersionIsEmitted(t *testing.T) {
	a := &Atlas{SchemaVersion: SchemaVersion, Company: "acme"}
	b, err := a.MarshalJSONIndent()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"schemaVersion": 1`) {
		t.Errorf("schemaVersion missing from output: %s", b)
	}
}

func TestOmittedOptionalFieldsAreAbsent(t *testing.T) {
	p := Package{Name: "a", Access: AccessPublic, Primitives: []Primitive{}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"reason", "install"} {
		if strings.Contains(string(b), `"`+absent+`"`) {
			t.Errorf("%q should be omitted when empty: %s", absent, b)
		}
	}
}

func TestUnavailableSourceCarriesReason(t *testing.T) {
	s := Source{Name: "x", Kind: "marketplace", Status: StatusUnavailable, Reason: "fetch failed: 404"}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"reason":"fetch failed: 404"`) {
		t.Errorf("reason must survive marshalling: %s", b)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd ~/workspace/atlas && go test ./internal/model/ -v`
Expected: FAIL — `undefined: Package`.

- [ ] **Step 3: Write the implementation**

```go
// Package model defines atlas.json — Atlas's public output schema.
//
// This is a contract others build on, so two distinctions are load-bearing and
// cannot change without a SchemaVersion bump:
//
//   - Primitives == nil  means "not harvested" (restricted or excluded).
//     Primitives == []   means "harvested, genuinely empty".
//   - Access "restricted" means Atlas could not read it; "excluded" means Atlas
//     could have read it but was told not to render it.
package model

import "encoding/json"

// SchemaVersion is the atlas.json contract version.
const SchemaVersion = 1

// Access values describe what this run did with a package, not the package's
// intended audience.
const (
	AccessPublic     = "public"     // harvested
	AccessRestricted = "restricted" // could not read (clone denied)
	AccessExcluded   = "excluded"   // could read; descriptor said withhold
)

// Source status values.
const (
	StatusRead        = "read"
	StatusUnavailable = "unavailable"
)

// Primitive types. This closed set is Atlas's own invention: no closed
// primitive-type enum exists upstream (manifest treats primitive.type as a
// free-form string), so this fills a gap rather than matching a convention.
const (
	TypeSkill   = "skill"
	TypeAgent   = "agent"
	TypeHook    = "hook"
	TypeCommand = "command"
	TypeMCP     = "mcp"
)

// Primitive is one governable unit inside a package.
type Primitive struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Install holds the commands rendered for a package. Both are derived from
// manifest fields; a missing command is correct, a guessed one is a defect.
type Install struct {
	MarketplaceAdd string `json:"marketplaceAdd"`
	Install        string `json:"install"`
}

// Package is one package (or, for a repo source, the implicit whole-repo
// package).
type Package struct {
	Name         string `json:"name"`
	Source       string `json:"source"`
	Description  string `json:"description,omitempty"`
	Version      string `json:"version,omitempty"`
	ResolvedFrom string `json:"resolvedFrom,omitempty"`
	ResolvedSha  string `json:"resolvedSha,omitempty"`
	Access       string `json:"access"`
	Reason       string `json:"reason,omitempty"`

	// Primitives is nil when not harvested and empty when harvested-but-empty.
	// No omitempty: the null must survive to distinguish the two.
	Primitives []Primitive `json:"primitives"`

	Install *Install `json:"install,omitempty"`
}

// Source is one descriptor source as resolved. An unavailable source still
// appears here, with a reason; it is never silently absent.
type Source struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	SourceBase string `json:"sourceBase,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Version    string `json:"version,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// Collision records a name clash. Atlas reports; a resolver decides.
type Collision struct {
	Kind    string   `json:"kind"` // "package-name" or "primitive-name"
	Name    string   `json:"name"`
	Sources []string `json:"sources"`
}

// Summary is the counts line, also printed to stderr by the CLI.
type Summary struct {
	Sources  map[string]int `json:"sources"`
	Packages map[string]int `json:"packages"`
}

// Atlas is the root of atlas.json.
type Atlas struct {
	SchemaVersion int         `json:"schemaVersion"`
	Company       string      `json:"company"`
	GeneratedAt   string      `json:"generatedAt"`
	Sources       []Source    `json:"sources"`
	Packages      []Package   `json:"packages"`
	Collisions    []Collision `json:"collisions"`
	Summary       Summary     `json:"summary"`
}

// MarshalJSONIndent renders atlas.json in its on-disk form.
func (a *Atlas) MarshalJSONIndent() ([]byte, error) {
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd ~/workspace/atlas && go test ./internal/model/ -v`
Expected: PASS — all four tests.

- [ ] **Step 5: Commit**

```bash
cd ~/workspace/atlas
git add internal/model/
git commit -m "feat(model): atlas.json public schema

Two distinctions are load-bearing and tested: primitives null (not harvested)
vs [] (harvested, empty), and access restricted (could not read) vs excluded
(could read, told to withhold). Neither can change without a schemaVersion bump.

Records that the closed primitive-type set is Atlas's own invention — no such
enum exists upstream."
```

---

### Task 4: Git operations — clone at a ref, resolve the SHA

**Files:**
- Create: `~/workspace/atlas/internal/gitc/gitc.go`
- Test: `~/workspace/atlas/internal/gitc/gitc_test.go`
- Test helper: `~/workspace/atlas/internal/gitc/fixture_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type CloneResult struct { Dir, Sha string }`
  - `func Clone(url, ref, destParent string) (*CloneResult, error)` — `ref` empty means default branch
  - `var ErrAccessDenied = errors.New("access denied")` — returned when the clone fails for auth/not-found reasons
  - `func NewFixtureRepo(t *testing.T, files map[string]string, tag string) string` (test helper, exported for reuse in Tasks 5–10)

- [ ] **Step 1: Write the fixture helper**

Real git repos on disk, cloned over `file://`. This exercises the actual clone path
including tag resolution — required for guarantee tests 5–8.

```go
package gitc

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// NewFixtureRepo creates a real git repo in a temp dir, writes files, commits,
// and optionally tags. Returns a file:// URL suitable for Clone.
func NewFixtureRepo(t *testing.T, files map[string]string, tag string) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.test",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.test",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-b", "main")
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-m", "fixture", "--no-gpg-sign")
	if tag != "" {
		run("tag", tag)
	}
	return "file://" + dir
}
```

- [ ] **Step 2: Write the failing tests**

```go
package gitc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCloneDefaultBranchRecordsSha(t *testing.T) {
	url := NewFixtureRepo(t, map[string]string{"README.md": "hi"}, "")
	res, err := Clone(url, "", t.TempDir())
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if len(res.Sha) != 40 {
		t.Errorf("Sha = %q, want a 40-char SHA", res.Sha)
	}
	if _, err := os.Stat(filepath.Join(res.Dir, "README.md")); err != nil {
		t.Errorf("cloned tree missing README.md: %v", err)
	}
}

func TestCloneAtTag(t *testing.T) {
	url := NewFixtureRepo(t, map[string]string{"a.txt": "x"}, "v1.2.3")
	res, err := Clone(url, "v1.2.3", t.TempDir())
	if err != nil {
		t.Fatalf("Clone at tag: %v", err)
	}
	if len(res.Sha) != 40 {
		t.Errorf("Sha = %q, want a 40-char SHA", res.Sha)
	}
}

func TestCloneMissingTagFails(t *testing.T) {
	url := NewFixtureRepo(t, map[string]string{"a.txt": "x"}, "v1.0.0")
	if _, err := Clone(url, "v9.9.9", t.TempDir()); err == nil {
		t.Fatal("expected error cloning a nonexistent tag")
	}
}

func TestCloneNonexistentRepoIsAccessDenied(t *testing.T) {
	_, err := Clone("file:///nonexistent-"+t.Name(), "", t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrAccessDenied) {
		t.Errorf("err = %v, want ErrAccessDenied", err)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd ~/workspace/atlas && go test ./internal/gitc/ -v`
Expected: FAIL — `undefined: Clone`.

- [ ] **Step 4: Write the implementation**

```go
// Package gitc runs the git binary. Shelling out rather than using a library is
// deliberate: the user's existing auth (SSH agent, credential helper, keychain)
// applies unchanged, which is what Atlas's host-agnostic requirement needs.
package gitc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrAccessDenied wraps any clone failure Atlas should treat as "cannot read
// this" rather than a hard error — auth failure, not-found, or a vanished repo.
// Callers render these as access "restricted".
var ErrAccessDenied = errors.New("access denied")

// CloneResult is where the tree landed and what commit it actually is.
type CloneResult struct {
	Dir string
	Sha string
}

// Clone shallow-clones url into a new directory under destParent. An empty ref
// clones the default branch. The resolved SHA is always recorded, because
// reproducibility comes from recording what was fetched, not from requiring
// tags.
func Clone(url, ref, destParent string) (*CloneResult, error) {
	dir, err := os.MkdirTemp(destParent, "atlas-clone-")
	if err != nil {
		return nil, fmt.Errorf("mkdir clone dest: %w", err)
	}

	args := []string{"clone", "--depth", "1", "--filter=blob:none", "--no-tags"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, url, dir)

	if out, err := run(dir, args...); err != nil {
		os.RemoveAll(dir)
		if isAccessFailure(out) {
			return nil, fmt.Errorf("%w: %s", ErrAccessDenied, firstLine(out))
		}
		return nil, fmt.Errorf("git clone %s: %v: %s", url, err, firstLine(out))
	}

	sha, err := run(dir, "rev-parse", "HEAD")
	if err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("resolve HEAD for %s: %w", url, err)
	}
	return &CloneResult{Dir: dir, Sha: strings.TrimSpace(sha)}, nil
}

// run executes git with global/system config neutralised, so a developer's
// personal git config cannot change what Atlas reads.
func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if _, err := os.Stat(dir); err == nil {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", // never block waiting for credentials
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// isAccessFailure distinguishes "you may not read this" from a real fault. The
// git binary reports both through exit status, so the message is all we have.
func isAccessFailure(out string) bool {
	s := strings.ToLower(out)
	for _, sig := range []string{
		"authentication failed",
		"permission denied",
		"could not read username",
		"access denied",
		"repository not found",
		"does not appear to be a git repository",
		"not found",
		"403",
		"404",
	} {
		if strings.Contains(s, sig) {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// Cleanup removes a cloned tree.
func Cleanup(r *CloneResult) {
	if r != nil && r.Dir != "" {
		os.RemoveAll(filepath.Clean(r.Dir))
	}
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd ~/workspace/atlas && go test ./internal/gitc/ -v`
Expected: PASS — all four tests.

- [ ] **Step 6: Commit**

```bash
cd ~/workspace/atlas
git add internal/gitc/
git commit -m "feat(gitc): shallow clone at a ref, always recording the SHA

Shells out to git so the user's existing auth applies unchanged — the
host-agnostic requirement. GIT_TERMINAL_PROMPT=0 so a missing credential fails
instead of hanging, and global/system config is neutralised so a developer's
personal git config cannot change what Atlas reads.

ErrAccessDenied separates 'may not read' from a real fault, which is what the
locked-card path keys on. Tests build real repos in t.TempDir() and clone over
file://, so tag resolution and the denied path are genuinely exercised."
```

---

### Task 5: Frontmatter harvesting

**Files:**
- Create: `~/workspace/atlas/internal/harvest/frontmatter.go`
- Test: `~/workspace/atlas/internal/harvest/frontmatter_test.go`

**Interfaces:**
- Consumes: `model.Primitive`, `model.TypeSkill` etc. from Task 3.
- Produces:
  - `func ParseFrontmatter(content []byte) (name, description string, err error)`

- [ ] **Step 1: Write the failing tests**

```go
package harvest

import "testing"

func TestParseFrontmatter(t *testing.T) {
	name, desc, err := ParseFrontmatter([]byte(`---
name: code-review
description: Review code changes with a rigorous methodology.
---

# Body text here
`))
	if err != nil {
		t.Fatalf("ParseFrontmatter: %v", err)
	}
	if name != "code-review" {
		t.Errorf("name = %q, want code-review", name)
	}
	if desc != "Review code changes with a rigorous methodology." {
		t.Errorf("description = %q", desc)
	}
}

func TestParseFrontmatterMultilineDescription(t *testing.T) {
	_, desc, err := ParseFrontmatter([]byte(`---
name: x
description: >-
  first line
  second line
---
body
`))
	if err != nil {
		t.Fatalf("ParseFrontmatter: %v", err)
	}
	if desc != "first line second line" {
		t.Errorf("description = %q, want folded scalar joined", desc)
	}
}

func TestParseFrontmatterMissingBlockIsError(t *testing.T) {
	if _, _, err := ParseFrontmatter([]byte("# no frontmatter\n")); err == nil {
		t.Fatal("expected error when frontmatter is absent")
	}
}

func TestParseFrontmatterUnterminatedIsError(t *testing.T) {
	if _, _, err := ParseFrontmatter([]byte("---\nname: x\nno closing fence\n")); err == nil {
		t.Fatal("expected error for unterminated frontmatter")
	}
}

func TestParseFrontmatterPreservesMarkupVerbatim(t *testing.T) {
	// Escaping is the renderer's job. The parser must not silently alter input,
	// or the escaping test in the render package would pass for the wrong reason.
	_, desc, err := ParseFrontmatter([]byte(`---
name: x
description: "uses <script>alert(1)</script> internally"
---
body
`))
	if err != nil {
		t.Fatalf("ParseFrontmatter: %v", err)
	}
	if desc != "uses <script>alert(1)</script> internally" {
		t.Errorf("description = %q, want verbatim markup", desc)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd ~/workspace/atlas && go test ./internal/harvest/ -v`
Expected: FAIL — `undefined: ParseFrontmatter`.

- [ ] **Step 3: Write the implementation**

```go
// Package harvest reads primitive metadata out of a cloned tree.
//
// Harvesting is not classifying: Atlas reads name and description from files
// and invents nothing. Values are returned verbatim — escaping belongs to the
// renderer.
package harvest

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

var fence = []byte("---")

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// ParseFrontmatter extracts name and description from a leading YAML
// frontmatter block. Missing or unterminated frontmatter is an error: a
// primitive Atlas cannot name is a primitive it must not silently list.
func ParseFrontmatter(content []byte) (string, string, error) {
	body := bytes.TrimLeft(content, "﻿ \t\r\n")
	if !bytes.HasPrefix(body, fence) {
		return "", "", fmt.Errorf("no frontmatter block")
	}
	rest := body[len(fence):]
	if len(rest) > 0 && rest[0] != '\n' && rest[0] != '\r' {
		return "", "", fmt.Errorf("no frontmatter block")
	}
	end := bytes.Index(rest, append([]byte("\n"), fence...))
	if end < 0 {
		return "", "", fmt.Errorf("unterminated frontmatter block")
	}
	var fm frontmatter
	if err := yaml.Unmarshal(rest[:end], &fm); err != nil {
		return "", "", fmt.Errorf("parse frontmatter: %w", err)
	}
	return fm.Name, fm.Description, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd ~/workspace/atlas && go test ./internal/harvest/ -v`
Expected: PASS — all five tests.

- [ ] **Step 5: Commit**

```bash
cd ~/workspace/atlas
git add internal/harvest/
git commit -m "feat(harvest): parse name and description from YAML frontmatter

Returns values verbatim, including markup: escaping is the renderer's job, and
a parser that sanitised here would make the renderer's escaping test pass for
the wrong reason. Missing or unterminated frontmatter is an error — a primitive
Atlas cannot name is one it must not silently list."
```

---

### Task 6: Walking a tree into primitives

**Files:**
- Create: `~/workspace/atlas/internal/harvest/walk.go`
- Test: `~/workspace/atlas/internal/harvest/walk_test.go`

**Interfaces:**
- Consumes: `ParseFrontmatter` (Task 5), `model.Primitive` (Task 3), `descriptor.Source` (Task 2).
- Produces:
  - `type WalkOptions struct { Exclude func(relPath string) bool }`
  - `func WalkTree(root string, opts WalkOptions) ([]model.Primitive, error)`

Recognised layouts, checked at `root` and at `root/.claude`:
`skills/<name>/SKILL.md` → skill · `agents/<name>.md` → agent · `commands/<name>.md` → command · `hooks/<file>` → hook (no frontmatter) · `.mcp.json` → mcp

- [ ] **Step 1: Write the failing tests**

```go
package harvest

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func names(ps []model.Primitive) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Type+":"+p.Name)
	}
	sort.Strings(out)
	return out
}

func TestWalkFindsAllPrimitiveTypes(t *testing.T) {
	root := tree(t, map[string]string{
		"skills/code-review/SKILL.md": "---\nname: code-review\ndescription: d1\n---\nbody",
		"agents/reviewer.md":          "---\nname: reviewer\ndescription: d2\n---\nbody",
		"commands/deploy.md":          "---\nname: deploy\ndescription: d3\n---\nbody",
		"hooks/guard.sh":              "#!/bin/sh\necho hi",
		".mcp.json":                   `{"mcpServers":{}}`,
	})
	got, err := WalkTree(root, WalkOptions{})
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}
	want := []string{"agent:reviewer", "command:deploy", "hook:guard.sh", "mcp:.mcp.json", "skill:code-review"}
	if g := names(got); len(g) != len(want) {
		t.Fatalf("got %v, want %v", g, want)
	}
}

func TestWalkFindsDotClaudeLayout(t *testing.T) {
	root := tree(t, map[string]string{
		".claude/skills/a/SKILL.md": "---\nname: a\ndescription: d\n---\nbody",
	})
	got, err := WalkTree(root, WalkOptions{})
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}
	if len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("got %v, want one skill named a", names(got))
	}
}

func TestWalkAppliesExclude(t *testing.T) {
	root := tree(t, map[string]string{
		"skills/finance-ops/SKILL.md": "---\nname: finance-ops\ndescription: secret\n---\nbody",
		"skills/code-review/SKILL.md": "---\nname: code-review\ndescription: fine\n---\nbody",
	})
	got, err := WalkTree(root, WalkOptions{
		Exclude: func(rel string) bool { return rel == "skills/finance-ops/SKILL.md" },
	})
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}
	for _, p := range got {
		if p.Name == "finance-ops" {
			t.Fatal("excluded primitive must not be returned")
		}
	}
	if len(got) != 1 {
		t.Fatalf("got %v, want only code-review", names(got))
	}
}

func TestWalkSkillMissingDescriptionIsError(t *testing.T) {
	root := tree(t, map[string]string{
		"skills/a/SKILL.md": "---\nname: a\n---\nbody",
	})
	if _, err := WalkTree(root, WalkOptions{}); err == nil {
		t.Fatal("expected error: a described-nothing primitive must fail closed")
	}
}

func TestWalkEmptyTreeReturnsEmptyNotNil(t *testing.T) {
	got, err := WalkTree(t.TempDir(), WalkOptions{})
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}
	if got == nil {
		t.Fatal("empty tree must return an empty slice, not nil — nil means 'not harvested'")
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", names(got))
	}
}
```

Add the import for `model` at the top of the test file:

```go
import "github.com/supermodular/atlas/internal/model"
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd ~/workspace/atlas && go test ./internal/harvest/ -run TestWalk -v`
Expected: FAIL — `undefined: WalkTree`.

- [ ] **Step 3: Write the implementation**

```go
package harvest

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/supermodular/atlas/internal/model"
)

// WalkOptions configures a tree walk. Exclude receives slash-separated paths
// relative to the walk root.
type WalkOptions struct {
	Exclude func(relPath string) bool
}

// WalkTree enumerates primitives in a cloned tree. It looks at the root and at
// root/.claude, so both a package layout and a raw repo layout are recognised.
//
// Returns an empty (non-nil) slice for a tree with no primitives: nil means
// "not harvested" in atlas.json and must not be produced by a successful walk.
func WalkTree(root string, opts WalkOptions) ([]model.Primitive, error) {
	found := []model.Primitive{}
	for _, base := range []string{root, filepath.Join(root, ".claude")} {
		if st, err := os.Stat(base); err != nil || !st.IsDir() {
			continue
		}
		ps, err := walkBase(root, base, opts)
		if err != nil {
			return nil, err
		}
		found = append(found, ps...)
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].Type != found[j].Type {
			return found[i].Type < found[j].Type
		}
		return found[i].Name < found[j].Name
	})
	return found, nil
}

func walkBase(root, base string, opts WalkOptions) ([]model.Primitive, error) {
	var out []model.Primitive

	rel := func(p string) string {
		r, err := filepath.Rel(root, p)
		if err != nil {
			return p
		}
		return filepath.ToSlash(r)
	}
	excluded := func(p string) bool {
		return opts.Exclude != nil && opts.Exclude(rel(p))
	}

	// skills/<name>/SKILL.md
	dirs, err := os.ReadDir(filepath.Join(base, "skills"))
	if err == nil {
		for _, d := range dirs {
			if !d.IsDir() {
				continue
			}
			p := filepath.Join(base, "skills", d.Name(), "SKILL.md")
			if excluded(p) {
				continue
			}
			prim, err := readDescribed(p, model.TypeSkill, d.Name())
			if err != nil {
				return nil, err
			}
			if prim != nil {
				out = append(out, *prim)
			}
		}
	}

	// agents/<name>.md and commands/<name>.md
	for dir, typ := range map[string]string{
		"agents":   model.TypeAgent,
		"commands": model.TypeCommand,
	} {
		entries, err := os.ReadDir(filepath.Join(base, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			p := filepath.Join(base, dir, e.Name())
			if excluded(p) {
				continue
			}
			prim, err := readDescribed(p, typ, strings.TrimSuffix(e.Name(), ".md"))
			if err != nil {
				return nil, err
			}
			if prim != nil {
				out = append(out, *prim)
			}
		}
	}

	// hooks/<file> — scripts carry no frontmatter, so name only.
	if entries, err := os.ReadDir(filepath.Join(base, "hooks")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			p := filepath.Join(base, "hooks", e.Name())
			if excluded(p) {
				continue
			}
			out = append(out, model.Primitive{Type: model.TypeHook, Name: e.Name()})
		}
	}

	// .mcp.json
	mcp := filepath.Join(base, ".mcp.json")
	if st, err := os.Stat(mcp); err == nil && !st.IsDir() && !excluded(mcp) {
		out = append(out, model.Primitive{Type: model.TypeMCP, Name: ".mcp.json"})
	}

	return out, nil
}

// readDescribed reads a frontmatter-bearing primitive. A primitive with no
// description fails the build: the upstream emitter throws rather than ship an
// undescribed package, and Atlas takes the same posture.
func readDescribed(p, typ, fallbackName string) (*model.Primitive, error) {
	content, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	name, desc, err := ParseFrontmatter(content)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path.Base(p), err)
	}
	if strings.TrimSpace(desc) == "" {
		return nil, fmt.Errorf("%s: %s %q has no description — add one before it can appear in an atlas",
			p, typ, fallbackName)
	}
	if strings.TrimSpace(name) == "" {
		name = fallbackName
	}
	return &model.Primitive{Type: typ, Name: name, Description: desc}, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd ~/workspace/atlas && go test ./internal/harvest/ -v`
Expected: PASS — all tests in the package.

- [ ] **Step 5: Commit**

```bash
cd ~/workspace/atlas
git add internal/harvest/
git commit -m "feat(harvest): walk a tree into typed primitives

Recognises both the package layout and a raw repo's .claude/ layout, so one
walker serves both source kinds. Excludes are applied at the path level before
any content is read, so an excluded primitive's description never enters memory.

An empty tree returns an empty slice, never nil: nil means 'not harvested' in
atlas.json, and a successful walk must not claim that. A primitive with no
description fails the build, matching the upstream emitter's posture."
```

---

### Task 7: Resolving marketplace manifests

**Files:**
- Create: `~/workspace/atlas/internal/resolve/marketplace.go`
- Test: `~/workspace/atlas/internal/resolve/marketplace_test.go`

**Interfaces:**
- Consumes: `gitc.Clone` (Task 4), `gitc.NewFixtureRepo` (test helper).
- Produces:
  - `type ManifestPackage struct { Name, Description, Source, Version string }`
  - `type Manifest struct { Name, Owner, Version, SourceBase, TagPattern string; Packages []ManifestPackage }`
  - `func ParseManifest(data []byte) (*Manifest, error)`
  - `func (m *Manifest) ResolveURL(p ManifestPackage) (string, error)`
  - `func (m *Manifest) ResolveRef(p ManifestPackage) string`

- [ ] **Step 1: Write the failing tests**

Fixture YAML mirrors the real published manifest's shape, including the bare
`source:` form that only resolves via `sourceBase`.

```go
package resolve

import (
	"strings"
	"testing"
)

const fixtureManifest = `
name: ai-primitives
version: 0.2.1
description: a marketplace
author:
  name: acme
license: UNLICENSED
includes: auto
marketplace:
  owner:
    name: acme
  sourceBase: https://git.example.test/acme/group
  build:
    tagPattern: "v{version}"
  outputs:
    claude: {}
  packages:
    - name: pkg-one
      description: "First package."
      source: pkg-one
      version: "0.2.1"
    - name: pkg-two
      description: "Second package."
      source: pkg-two
      version: "0.1.0"
`

func TestParseManifest(t *testing.T) {
	m, err := ParseManifest([]byte(fixtureManifest))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Name != "ai-primitives" {
		t.Errorf("Name = %q", m.Name)
	}
	if m.Owner != "acme" {
		t.Errorf("Owner = %q", m.Owner)
	}
	if m.SourceBase != "https://git.example.test/acme/group" {
		t.Errorf("SourceBase = %q", m.SourceBase)
	}
	if m.TagPattern != "v{version}" {
		t.Errorf("TagPattern = %q", m.TagPattern)
	}
	if len(m.Packages) != 2 {
		t.Fatalf("len(Packages) = %d, want 2", len(m.Packages))
	}
}

func TestResolveURLConcatenatesSourceBase(t *testing.T) {
	m, err := ParseManifest([]byte(fixtureManifest))
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.ResolveURL(m.Packages[0])
	if err != nil {
		t.Fatalf("ResolveURL: %v", err)
	}
	want := "https://git.example.test/acme/group/pkg-one"
	if got != want {
		t.Errorf("ResolveURL = %q, want %q", got, want)
	}
}

func TestResolveURLAcceptsFullyQualifiedSource(t *testing.T) {
	m := &Manifest{}
	got, err := m.ResolveURL(ManifestPackage{Name: "p", Source: "https://git.example.test/x/p"})
	if err != nil {
		t.Fatalf("ResolveURL: %v", err)
	}
	if got != "https://git.example.test/x/p" {
		t.Errorf("ResolveURL = %q", got)
	}
}

func TestResolveURLBareSourceWithoutSourceBaseIsError(t *testing.T) {
	m := &Manifest{}
	_, err := m.ResolveURL(ManifestPackage{Name: "p", Source: "p"})
	if err == nil {
		t.Fatal("expected error: a bare source needs sourceBase to resolve")
	}
}

func TestResolveRefAppliesTagPattern(t *testing.T) {
	m, err := ParseManifest([]byte(fixtureManifest))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ResolveRef(m.Packages[1]); got != "v0.1.0" {
		t.Errorf("ResolveRef = %q, want v0.1.0", got)
	}
}

func TestResolveRefEmptyWhenNoTagPattern(t *testing.T) {
	m := &Manifest{}
	if got := m.ResolveRef(ManifestPackage{Version: "1.0.0"}); got != "" {
		t.Errorf("ResolveRef = %q, want empty (clone default branch)", got)
	}
}

func TestParseManifestRejectsPackageWithoutName(t *testing.T) {
	_, err := ParseManifest([]byte(`
marketplace:
  packages:
    - description: "nameless"
      source: x
`))
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("expected a name-required error, got %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd ~/workspace/atlas && go test ./internal/resolve/ -v`
Expected: FAIL — `undefined: ParseManifest`.

- [ ] **Step 3: Write the implementation**

```go
// Package resolve turns a descriptor source into a list of packages to harvest.
package resolve

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManifestPackage is one entry in a published marketplace's package list.
type ManifestPackage struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Source      string `yaml:"source"`
	Version     string `yaml:"version"`
}

// Manifest is the subset of a published APM marketplace manifest Atlas reads.
type Manifest struct {
	Name       string
	Owner      string
	Version    string
	SourceBase string
	TagPattern string
	Packages   []ManifestPackage
}

// rawManifest mirrors the on-disk YAML shape.
type rawManifest struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Marketplace struct {
		Owner struct {
			Name string `yaml:"name"`
		} `yaml:"owner"`
		SourceBase string `yaml:"sourceBase"`
		Build      struct {
			TagPattern string `yaml:"tagPattern"`
		} `yaml:"build"`
		Packages []ManifestPackage `yaml:"packages"`
	} `yaml:"marketplace"`
}

// ParseManifest reads a published marketplace manifest.
func ParseManifest(data []byte) (*Manifest, error) {
	var raw rawManifest
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	m := &Manifest{
		Name:       raw.Name,
		Owner:      raw.Marketplace.Owner.Name,
		Version:    raw.Version,
		SourceBase: strings.TrimRight(raw.Marketplace.SourceBase, "/"),
		TagPattern: raw.Marketplace.Build.TagPattern,
		Packages:   raw.Marketplace.Packages,
	}
	for i, p := range m.Packages {
		if strings.TrimSpace(p.Name) == "" {
			return nil, fmt.Errorf("manifest: packages[%d]: name is required", i)
		}
	}
	return m, nil
}

// ResolveURL turns a package's source into a clone URL.
//
// A bare source (no slashes) is the normal form for deeply-nested namespaces,
// because the default "<owner>/<repo>" form accepts exactly two path segments.
// Such a source resolves only as sourceBase + "/" + source, which is why a bare
// source without a sourceBase is an error rather than a guess.
func (m *Manifest) ResolveURL(p ManifestPackage) (string, error) {
	src := strings.TrimSpace(p.Source)
	if src == "" {
		src = p.Name
	}
	if strings.Contains(src, "://") || strings.HasPrefix(src, "git@") {
		return src, nil
	}
	if m.SourceBase == "" {
		return "", fmt.Errorf("package %q has a relative source %q but the manifest declares no sourceBase", p.Name, src)
	}
	return m.SourceBase + "/" + strings.TrimLeft(src, "/"), nil
}

// ResolveRef returns the git ref to clone, or "" for the default branch.
// Atlas never requires a tag: reproducibility comes from recording the resolved
// SHA, not from demanding a tag exist.
func (m *Manifest) ResolveRef(p ManifestPackage) string {
	if m.TagPattern == "" || p.Version == "" {
		return ""
	}
	return strings.ReplaceAll(m.TagPattern, "{version}", p.Version)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd ~/workspace/atlas && go test ./internal/resolve/ -v`
Expected: PASS — all seven tests.

- [ ] **Step 5: Commit**

```bash
cd ~/workspace/atlas
git add internal/resolve/
git commit -m "feat(resolve): parse a published marketplace manifest

A bare source resolves only as sourceBase + '/' + source — the normal form for
deeply-nested namespaces, since the default two-segment owner/repo form cannot
express them. A bare source with no sourceBase is therefore an error, not a
guess: a wrong clone URL on a governance page is worse than a stated failure.

No tag is required. Empty ref means default branch, and the resolved SHA is
recorded either way."
```

---

### Task 8: Building the atlas — merge, provenance, collisions

**Files:**
- Create: `~/workspace/atlas/internal/build/build.go`
- Test: `~/workspace/atlas/internal/build/build_test.go`

**Interfaces:**
- Consumes: everything from Tasks 2–7.
- Produces:
  - `type Options struct { Descriptor *descriptor.Descriptor; Now func() string; WorkDir string }`
  - `func Build(opts Options) (*model.Atlas, error)`
  - `func DetectCollisions(pkgs []model.Package) []model.Collision`

- [ ] **Step 1: Write the failing collision tests first (pure, no network)**

```go
package build

import (
	"testing"

	"github.com/supermodular/atlas/internal/model"
)

func TestDetectPackageNameCollision(t *testing.T) {
	got := DetectCollisions([]model.Package{
		{Name: "dup", Source: "a", Primitives: []model.Primitive{}},
		{Name: "dup", Source: "b", Primitives: []model.Primitive{}},
		{Name: "solo", Source: "a", Primitives: []model.Primitive{}},
	})
	if len(got) != 1 {
		t.Fatalf("got %d collisions, want 1: %+v", len(got), got)
	}
	if got[0].Kind != "package-name" || got[0].Name != "dup" {
		t.Errorf("collision = %+v", got[0])
	}
	if len(got[0].Sources) != 2 {
		t.Errorf("Sources = %v, want two", got[0].Sources)
	}
}

func TestDetectPrimitiveNameCollisionAcrossPackages(t *testing.T) {
	got := DetectCollisions([]model.Package{
		{Name: "p1", Source: "a", Primitives: []model.Primitive{{Type: "skill", Name: "shared"}}},
		{Name: "p2", Source: "a", Primitives: []model.Primitive{{Type: "skill", Name: "shared"}}},
	})
	if len(got) != 1 || got[0].Kind != "primitive-name" {
		t.Fatalf("got %+v, want one primitive-name collision", got)
	}
}

func TestNoCollisionWithinOnePackage(t *testing.T) {
	// The same name at two types in one package is not a clash a consumer hits.
	got := DetectCollisions([]model.Package{
		{Name: "p", Source: "a", Primitives: []model.Primitive{
			{Type: "skill", Name: "x"},
			{Type: "hook", Name: "x"},
		}},
	})
	if len(got) != 0 {
		t.Fatalf("got %+v, want none", got)
	}
}

func TestCollisionsIgnoreWithheldPackages(t *testing.T) {
	// A withheld package has no primitives to clash, and its name appearing
	// twice is still a real package-name clash — but nil primitives must not
	// panic or invent a primitive collision.
	got := DetectCollisions([]model.Package{
		{Name: "p", Source: "a", Access: model.AccessExcluded, Primitives: nil},
		{Name: "q", Source: "b", Access: model.AccessRestricted, Primitives: nil},
	})
	if len(got) != 0 {
		t.Fatalf("got %+v, want none", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd ~/workspace/atlas && go test ./internal/build/ -v`
Expected: FAIL — `undefined: DetectCollisions`.

- [ ] **Step 3: Implement `DetectCollisions`**

```go
// Package build runs the pipeline: resolve, harvest, merge.
package build

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/supermodular/atlas/internal/descriptor"
	"github.com/supermodular/atlas/internal/gitc"
	"github.com/supermodular/atlas/internal/harvest"
	"github.com/supermodular/atlas/internal/model"
	"github.com/supermodular/atlas/internal/resolve"
)

// Collision kinds.
const (
	CollisionPackageName   = "package-name"
	CollisionPrimitiveName = "primitive-name"
)

// DetectCollisions finds name clashes across sources and packages. Atlas
// reports clashes; it never resolves them — a package name is only meaningful
// relative to its source, so a union can legitimately hold two of the same name.
func DetectCollisions(pkgs []model.Package) []model.Collision {
	var out []model.Collision

	pkgSources := map[string]map[string]bool{}
	for _, p := range pkgs {
		if pkgSources[p.Name] == nil {
			pkgSources[p.Name] = map[string]bool{}
		}
		pkgSources[p.Name][p.Source] = true
	}
	for name, srcs := range pkgSources {
		if len(srcs) > 1 {
			out = append(out, model.Collision{
				Kind: CollisionPackageName, Name: name, Sources: sortedKeys(srcs),
			})
		}
	}

	// Primitive names clash only across different packages: the same name at
	// two types inside one package is not something a consumer trips over.
	primOwners := map[string]map[string]bool{}
	for _, p := range pkgs {
		for _, prim := range p.Primitives {
			key := prim.Type + ":" + prim.Name
			if primOwners[key] == nil {
				primOwners[key] = map[string]bool{}
			}
			primOwners[key][p.Source+"/"+p.Name] = true
		}
	}
	for key, owners := range primOwners {
		if len(owners) > 1 {
			out = append(out, model.Collision{
				Kind: CollisionPrimitiveName, Name: key, Sources: sortedKeys(owners),
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Run to verify the collision tests pass**

Run: `cd ~/workspace/atlas && go test ./internal/build/ -v`
Expected: PASS — four collision tests.

- [ ] **Step 5: Write the failing end-to-end build tests**

These are guarantee tests 5–8 from the spec. Test 8 is the critical one: the
fixture **grants** access, so exclusion must be enforced by the descriptor rather
than incidentally by a failed clone.

```go
package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/supermodular/atlas/internal/descriptor"
	"github.com/supermodular/atlas/internal/gitc"
	"github.com/supermodular/atlas/internal/model"
)

func fixedNow() string { return "2026-08-18T00:00:00Z" }

func writeDescriptor(t *testing.T, body string) *descriptor.Descriptor {
	t.Helper()
	p := filepath.Join(t.TempDir(), "d.yml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := descriptor.Load(p)
	if err != nil {
		t.Fatalf("descriptor.Load: %v", err)
	}
	return d
}

// jsonOf marshals the atlas so tests can assert on the exact bytes shipped.
func jsonOf(t *testing.T, a *model.Atlas) string {
	t.Helper()
	b, err := a.MarshalJSONIndent()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBuildRepoSourceHarvests(t *testing.T) {
	repo := gitc.NewFixtureRepo(t, map[string]string{
		".claude/skills/code-review/SKILL.md": "---\nname: code-review\ndescription: Reviews code.\n---\nbody",
	}, "")
	d := writeDescriptor(t, `
company: acme
sources:
  - kind: repo
    name: r
    url: `+repo+`
    acknowledgeUnclassified: true
`)
	a, err := Build(Options{Descriptor: d, Now: fixedNow, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(a.Packages) != 1 {
		t.Fatalf("got %d packages, want 1", len(a.Packages))
	}
	p := a.Packages[0]
	if p.Access != model.AccessPublic {
		t.Errorf("Access = %q, want public", p.Access)
	}
	if len(p.Primitives) != 1 || p.Primitives[0].Name != "code-review" {
		t.Errorf("Primitives = %+v", p.Primitives)
	}
	if len(p.ResolvedSha) != 40 {
		t.Errorf("ResolvedSha = %q, want a full SHA", p.ResolvedSha)
	}
}

// Guarantee test 6: fail closed on an unclassified repo.
func TestBuildRefusesUnacknowledgedUnclassifiedRepo(t *testing.T) {
	repo := gitc.NewFixtureRepo(t, map[string]string{
		".claude/skills/a/SKILL.md": "---\nname: a\ndescription: d\n---\nbody",
	}, "")
	d := writeDescriptor(t, `
company: acme
sources:
  - kind: repo
    name: r
    url: `+repo+`
`)
	if _, err := Build(Options{Descriptor: d, Now: fixedNow, WorkDir: t.TempDir()}); err == nil {
		t.Fatal("expected refusal: no classification, no excludes, no acknowledgement")
	}
}

// Guarantee test 8, the critical one: exclusion must beat a SUCCESSFUL clone.
func TestBuildMarketplaceExcludeBeatsSuccessfulClone(t *testing.T) {
	secret := gitc.NewFixtureRepo(t, map[string]string{
		"skills/finance-ops/SKILL.md": "---\nname: finance-ops\ndescription: SECRETVALUE.\n---\nbody",
	}, "v1.0.0")
	open := gitc.NewFixtureRepo(t, map[string]string{
		"skills/code-review/SKILL.md": "---\nname: code-review\ndescription: Fine.\n---\nbody",
	}, "v1.0.0")

	// A manifest whose sources are absolute file:// URLs, both readable.
	mkt := gitc.NewFixtureRepo(t, map[string]string{
		"apm.yml": `
name: mkt
version: 1.0.0
marketplace:
  owner:
    name: acme
  build:
    tagPattern: "v{version}"
  packages:
    - name: pkg-secret
      description: "Confidential."
      source: ` + secret + `
      version: "1.0.0"
    - name: pkg-open
      description: "Open."
      source: ` + open + `
      version: "1.0.0"
`,
	}, "")

	d := writeDescriptor(t, `
company: acme
sources:
  - kind: marketplace
    name: mkt
    url: `+mkt+`
    exclude:
      - pkg-secret
`)
	a, err := Build(Options{Descriptor: d, Now: fixedNow, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	out := jsonOf(t, a)
	if strings.Contains(out, "SECRETVALUE") {
		t.Error("excluded package's primitive description leaked into atlas.json")
	}
	if strings.Contains(out, "finance-ops") {
		t.Error("excluded package's primitive NAME leaked into atlas.json")
	}

	var secretPkg *model.Package
	for i := range a.Packages {
		if a.Packages[i].Name == "pkg-secret" {
			secretPkg = &a.Packages[i]
		}
	}
	if secretPkg == nil {
		t.Fatal("excluded package must still appear as a card, name and description only")
	}
	if secretPkg.Access != model.AccessExcluded {
		t.Errorf("Access = %q, want excluded", secretPkg.Access)
	}
	if secretPkg.Primitives != nil {
		t.Errorf("Primitives = %+v, want nil (withheld)", secretPkg.Primitives)
	}
	if secretPkg.Description == "" {
		t.Error("manifest description should survive on an excluded card")
	}
}

// Guarantee test 2: an unavailable source is recorded, never silently absent.
func TestBuildUnavailableSourceIsRecorded(t *testing.T) {
	d := writeDescriptor(t, `
company: acme
sources:
  - kind: marketplace
    name: gone
    url: file:///nonexistent-atlas-fixture
`)
	a, err := Build(Options{Descriptor: d, Now: fixedNow, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build should continue past an unavailable source: %v", err)
	}
	if len(a.Sources) != 1 {
		t.Fatalf("got %d sources, want 1", len(a.Sources))
	}
	if a.Sources[0].Status != model.StatusUnavailable {
		t.Errorf("Status = %q, want unavailable", a.Sources[0].Status)
	}
	if a.Sources[0].Reason == "" {
		t.Error("an unavailable source must carry a reason")
	}
	if len(a.Packages) != 0 {
		t.Errorf("an unavailable source must contribute no packages, got %d", len(a.Packages))
	}
	if a.Summary.Sources["unavailable"] != 1 {
		t.Errorf("Summary.Sources = %+v", a.Summary.Sources)
	}
}

// Guarantee test 1: a package Atlas cannot read is locked, not harvested.
func TestBuildRestrictedPackageIsLocked(t *testing.T) {
	mkt := gitc.NewFixtureRepo(t, map[string]string{
		"apm.yml": `
name: mkt
version: 1.0.0
marketplace:
  owner:
    name: acme
  packages:
    - name: pkg-gone
      description: "Exists per the manifest."
      source: file:///nonexistent-atlas-package
`,
	}, "")
	d := writeDescriptor(t, `
company: acme
sources:
  - kind: marketplace
    name: mkt
    url: `+mkt+`
`)
	a, err := Build(Options{Descriptor: d, Now: fixedNow, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(a.Packages) != 1 {
		t.Fatalf("got %d packages, want 1", len(a.Packages))
	}
	p := a.Packages[0]
	if p.Access != model.AccessRestricted {
		t.Errorf("Access = %q, want restricted", p.Access)
	}
	if p.Primitives != nil {
		t.Errorf("Primitives = %+v, want nil", p.Primitives)
	}
	if p.Description == "" {
		t.Error("manifest description should survive on a locked card")
	}
	if a.Summary.Packages["restricted"] != 1 {
		t.Errorf("Summary.Packages = %+v", a.Summary.Packages)
	}
}
```

- [ ] **Step 6: Run to verify they fail**

Run: `cd ~/workspace/atlas && go test ./internal/build/ -run TestBuild -v`
Expected: FAIL — `undefined: Build`.

- [ ] **Step 7: Implement `Build`**

Append to `internal/build/build.go`:

```go
// Options configures a build. Now is injectable so tests get a stable stamp.
type Options struct {
	Descriptor *descriptor.Descriptor
	Now        func() string
	WorkDir    string
}

// Build runs the pipeline and returns the atlas.
//
// Degradation is recorded, never fatal: an unreachable source becomes
// "unavailable" and a denied package becomes "restricted", both visible in the
// output. Only a misconfiguration — a repo source with no classification signal
// and no acknowledgement — aborts the run.
func Build(opts Options) (*model.Atlas, error) {
	if opts.Descriptor == nil {
		return nil, fmt.Errorf("build: descriptor is required")
	}
	now := opts.Now
	if now == nil {
		return nil, fmt.Errorf("build: Now is required")
	}
	work := opts.WorkDir
	if work == "" {
		var err error
		if work, err = os.MkdirTemp("", "atlas-work-"); err != nil {
			return nil, fmt.Errorf("build: work dir: %w", err)
		}
		defer os.RemoveAll(work)
	}

	a := &model.Atlas{
		SchemaVersion: model.SchemaVersion,
		Company:       opts.Descriptor.Company,
		GeneratedAt:   now(),
		Sources:       []model.Source{},
		Packages:      []model.Package{},
		Collisions:    []model.Collision{},
		Summary: model.Summary{
			Sources:  map[string]int{"read": 0, "unavailable": 0},
			Packages: map[string]int{"harvested": 0, "restricted": 0, "excluded": 0},
		},
	}

	for _, src := range opts.Descriptor.Sources {
		var (
			ms   model.Source
			pkgs []model.Package
			err  error
		)
		switch src.Kind {
		case descriptor.KindMarketplace:
			ms, pkgs, err = buildMarketplace(src, work)
		case descriptor.KindRepo:
			ms, pkgs, err = buildRepo(src, work)
		}
		if err != nil {
			return nil, err // misconfiguration, not degradation
		}
		a.Sources = append(a.Sources, ms)
		a.Summary.Sources[ms.Status]++
		a.Packages = append(a.Packages, pkgs...)
	}

	for _, p := range a.Packages {
		switch p.Access {
		case model.AccessPublic:
			a.Summary.Packages["harvested"]++
		case model.AccessRestricted:
			a.Summary.Packages["restricted"]++
		case model.AccessExcluded:
			a.Summary.Packages["excluded"]++
		}
	}

	if c := DetectCollisions(a.Packages); len(c) > 0 {
		a.Collisions = c
	}
	return a, nil
}

func buildMarketplace(src descriptor.Source, work string) (model.Source, []model.Package, error) {
	ms := model.Source{Name: src.Name, Kind: src.Kind}

	clone, err := gitc.Clone(src.URL, "", work)
	if err != nil {
		ms.Status = model.StatusUnavailable
		ms.Reason = err.Error()
		return ms, nil, nil
	}
	defer gitc.Cleanup(clone)

	data, err := readManifest(clone.Dir)
	if err != nil {
		ms.Status = model.StatusUnavailable
		ms.Reason = err.Error()
		return ms, nil, nil
	}
	man, err := resolve.ParseManifest(data)
	if err != nil {
		ms.Status = model.StatusUnavailable
		ms.Reason = err.Error()
		return ms, nil, nil
	}

	ms.Status = model.StatusRead
	ms.SourceBase = man.SourceBase
	ms.Owner = man.Owner
	ms.Version = man.Version

	var pkgs []model.Package
	for _, mp := range man.Packages {
		p := model.Package{
			Name:        mp.Name,
			Source:      src.Name,
			Description: mp.Description,
			Version:     mp.Version,
		}

		if src.IsExcluded(mp.Name) {
			p.Access = model.AccessExcluded
			p.Reason = "excluded by descriptor"
			p.Primitives = nil
			pkgs = append(pkgs, p)
			continue
		}

		url, err := man.ResolveURL(mp)
		if err != nil {
			p.Access = model.AccessRestricted
			p.Reason = err.Error()
			pkgs = append(pkgs, p)
			continue
		}
		p.ResolvedFrom = url

		pc, err := gitc.Clone(url, man.ResolveRef(mp), work)
		if err != nil {
			p.Access = model.AccessRestricted
			p.Reason = err.Error()
			if !errors.Is(err, gitc.ErrAccessDenied) {
				p.Reason = "clone failed: " + err.Error()
			}
			pkgs = append(pkgs, p)
			continue
		}
		p.ResolvedSha = pc.Sha

		prims, err := harvest.WalkTree(pc.Dir, harvest.WalkOptions{})
		gitc.Cleanup(pc)
		if err != nil {
			return ms, nil, fmt.Errorf("harvest %s: %w", mp.Name, err)
		}
		p.Access = model.AccessPublic
		p.Primitives = prims
		p.Install = &model.Install{
			MarketplaceAdd: fmt.Sprintf("apm marketplace add %s --name %s", src.URL, src.Name),
			Install:        fmt.Sprintf("apm install %s@%s --target claude", mp.Name, src.Name),
		}
		pkgs = append(pkgs, p)
	}
	return ms, pkgs, nil
}

func buildRepo(src descriptor.Source, work string) (model.Source, []model.Package, error) {
	ms := model.Source{Name: src.Name, Kind: src.Kind}

	clone, err := gitc.Clone(src.URL, "", work)
	if err != nil {
		ms.Status = model.StatusUnavailable
		ms.Reason = err.Error()
		return ms, nil, nil
	}
	defer gitc.Cleanup(clone)

	// Fail closed on the unknown case: a repo with no classification signal and
	// no excludes must be acknowledged in the descriptor, on the record.
	hasClassification := findClassification(clone.Dir) != ""
	if !hasClassification && len(src.Exclude) == 0 && !src.AcknowledgeUnclassified {
		return ms, nil, fmt.Errorf(
			"source %q: repo has no classification file and the descriptor sets no exclude rules; "+
				"add exclude globs or set acknowledgeUnclassified: true to record that rendering "+
				"everything present is intended", src.Name)
	}

	ms.Status = model.StatusRead
	prims, err := harvest.WalkTree(clone.Dir, harvest.WalkOptions{
		Exclude: func(rel string) bool { return src.IsExcluded(rel) },
	})
	if err != nil {
		return ms, nil, fmt.Errorf("harvest %s: %w", src.Name, err)
	}

	return ms, []model.Package{{
		Name:         src.Name,
		Source:       src.Name,
		Access:       model.AccessPublic,
		ResolvedFrom: src.URL,
		ResolvedSha:  clone.Sha,
		Primitives:   prims,
	}}, nil
}

// readManifest finds the marketplace manifest in a cloned marketplace repo.
func readManifest(dir string) ([]byte, error) {
	for _, rel := range []string{
		"apm.yml",
		filepath.Join("apm-catalog", "apm.yml"),
		"apm.yaml",
	} {
		if b, err := os.ReadFile(filepath.Join(dir, rel)); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("no apm.yml found in marketplace repo")
}

// findClassification looks for a classification file Atlas can obey. Atlas
// never classifies; it only honours a classification already written down.
func findClassification(dir string) string {
	for _, rel := range []string{
		"profiles.json",
		filepath.Join("os-dist", "profiles.json"),
		filepath.Join(".claude", "profiles.json"),
	} {
		p := filepath.Join(dir, rel)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}
```

- [ ] **Step 8: Run all build tests**

Run: `cd ~/workspace/atlas && go test ./internal/build/ -v`
Expected: PASS — all nine tests (four collision + five build).

- [ ] **Step 9: Commit**

```bash
cd ~/workspace/atlas
git add internal/build/
git commit -m "feat(build): merge sources into an atlas with provenance and collisions

Degradation is recorded, never fatal: an unreachable source becomes unavailable
and a denied package restricted, both visible in the output with a reason. Only
misconfiguration aborts — a repo source with no classification signal, no
excludes, and no acknowledgement, which fails closed by design.

The critical test asserts exclusion beats a SUCCESSFUL clone: the fixture grants
access, so a passing result proves the descriptor enforced it rather than a
failed clone doing so incidentally. That is the case that matters now that the
confidential packages moved into a group inheriting ~22 Developer+ members."
```

---

### Task 9: Rendering the site

**Files:**
- Create: `~/workspace/atlas/internal/render/render.go`
- Create: `~/workspace/atlas/internal/render/page.gohtml`
- Test: `~/workspace/atlas/internal/render/render_test.go`

**Interfaces:**
- Consumes: `model.Atlas` (Task 3).
- Produces: `func Render(a *model.Atlas) ([]byte, error)`

- [ ] **Step 1: Write the failing tests**

```go
package render

import (
	"strings"
	"testing"

	"github.com/supermodular/atlas/internal/model"
)

func sample() *model.Atlas {
	return &model.Atlas{
		SchemaVersion: model.SchemaVersion,
		Company:       "acme",
		GeneratedAt:   "2026-08-18T00:00:00Z",
		Sources: []model.Source{
			{Name: "mkt", Kind: "marketplace", Status: model.StatusRead, Owner: "acme", Version: "1.0.0"},
			{Name: "gone", Kind: "marketplace", Status: model.StatusUnavailable, Reason: "fetch failed: 404"},
		},
		Packages: []model.Package{
			{Name: "pkg-open", Source: "mkt", Description: "Open pkg.", Access: model.AccessPublic,
				ResolvedSha: "abc1234def5678", Primitives: []model.Primitive{
					{Type: model.TypeSkill, Name: "code-review", Description: "Reviews code."},
				},
				Install: &model.Install{MarketplaceAdd: "apm marketplace add u --name mkt", Install: "apm install pkg-open@mkt --target claude"}},
			{Name: "pkg-secret", Source: "mkt", Description: "Confidential.", Access: model.AccessExcluded,
				Reason: "excluded by descriptor", Primitives: nil},
			{Name: "pkg-locked", Source: "mkt", Description: "Unreadable.", Access: model.AccessRestricted,
				Reason: "clone failed: 403", Primitives: nil},
		},
		Collisions: []model.Collision{
			{Kind: "package-name", Name: "dup", Sources: []string{"mkt", "other"}},
		},
		Summary: model.Summary{
			Sources:  map[string]int{"read": 1, "unavailable": 1},
			Packages: map[string]int{"harvested": 1, "restricted": 1, "excluded": 1},
		},
	}
}

func TestRenderIncludesCoreContent(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		"acme", "pkg-open", "code-review", "Reviews code.",
		"pkg-secret", "pkg-locked", "dup",
		"apm install pkg-open@mkt --target claude",
		"2026-08-18T00:00:00Z",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rendered page missing %q", want)
		}
	}
}

func TestRenderIsSelfContained(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, forbidden := range []string{"http://", "https://cdn", "<script src", "<link rel=\"stylesheet\" href"} {
		if strings.Contains(s, forbidden) {
			t.Errorf("page must make no external requests, found %q", forbidden)
		}
	}
}

// Guarantee test 4: harvested markup must be inert.
func TestRenderEscapesHarvestedMarkup(t *testing.T) {
	a := sample()
	a.Packages[0].Primitives[0].Description = `<script>alert(1)</script>`
	out, err := Render(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "<script>alert(1)</script>") {
		t.Error("harvested markup was rendered live — it must be escaped")
	}
	if !strings.Contains(s, "&lt;script&gt;") {
		t.Error("expected the markup escaped into entities")
	}
}

func TestRenderDistinguishesExcludedFromRestricted(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "access restricted") {
		t.Error("a restricted package must say so on the page")
	}
	if !strings.Contains(s, "withheld by descriptor") {
		t.Error("an excluded package must be distinguishable from a restricted one")
	}
}

func TestRenderStatesTheClaimBoundary(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "not assert") {
		t.Error("the page must state what Atlas does not assert (spec §9)")
	}
}

func TestRenderNeverClaimsApproval(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(out))
	// "approved" may appear only inside the explicit disclaimer sentence.
	for _, banned := range []string{"approved primitive", "verified unaltered", "tamper-evident"} {
		if strings.Contains(lower, banned) {
			t.Errorf("page must not claim %q", banned)
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd ~/workspace/atlas && go test ./internal/render/ -v`
Expected: FAIL — `undefined: Render`.

- [ ] **Step 3: Write the template**

`internal/render/page.gohtml` — `html/template` escapes every `{{ }}` by default,
which is the injection guard. Never switch this to `text/template`.

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{ .Company }} — primitive atlas</title>
<style>
:root {
  --bg: #fbfbfa; --surface: #fff; --border: #e4e4e2; --text: #1a1a19;
  --muted: #6b6b68; --accent: #2b5fd9; --warn-bg: #fff8e6; --warn-border: #e0c068;
  --lock-bg: #f4f4f2;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #161614; --surface: #1e1e1c; --border: #33332f; --text: #ececea;
    --muted: #9a9a95; --accent: #8fa9f0; --warn-bg: #2c2413; --warn-border: #6b5722;
    --lock-bg: #232320;
  }
}
* { box-sizing: border-box; }
body {
  margin: 0; padding: 2rem 1.25rem; background: var(--bg); color: var(--text);
  font: 15px/1.55 ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
}
main { max-width: 60rem; margin: 0 auto; }
h1 { font-size: 1.6rem; margin: 0 0 .25rem; }
h2 { font-size: 1.1rem; margin: 2rem 0 .75rem; }
.meta, .claim { color: var(--muted); font-size: .84rem; }
.claim { margin: .75rem 0 0; padding: .7rem .85rem; border-left: 2px solid var(--border); }
code, .mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: .85em; }
.notice {
  margin: 1.25rem 0; padding: .8rem 1rem; background: var(--warn-bg);
  border: 1px solid var(--warn-border); border-radius: 6px; font-size: .88rem;
}
.card {
  background: var(--surface); border: 1px solid var(--border); border-radius: 8px;
  padding: 1rem 1.1rem; margin: .75rem 0;
}
.card.withheld { background: var(--lock-bg); }
.card h3 { margin: 0 0 .3rem; font-size: 1rem; }
.tag {
  display: inline-block; font-size: .72rem; padding: .1rem .45rem; border-radius: 4px;
  border: 1px solid var(--border); color: var(--muted); margin-left: .4rem; vertical-align: 1px;
}
.desc { margin: .35rem 0 .6rem; }
ul.prims { list-style: none; margin: .5rem 0 0; padding: 0; }
ul.prims li { padding: .3rem 0; border-top: 1px solid var(--border); }
.ptype {
  display: inline-block; min-width: 4.5rem; color: var(--muted);
  font-size: .74rem; text-transform: uppercase; letter-spacing: .04em;
}
pre {
  background: var(--lock-bg); border: 1px solid var(--border); border-radius: 6px;
  padding: .55rem .7rem; overflow-x: auto; margin: .5rem 0 0; font-size: .82rem;
}
.unavailable { border-style: dashed; }
</style>
</head>
<body>
<main>
  <h1>{{ .Company }} — primitive atlas</h1>
  <p class="meta">
    Generated {{ .GeneratedAt }} · schema v{{ .SchemaVersion }} ·
    {{ .Summary.Sources.read }} of {{ len .Sources }} sources read ·
    {{ .Summary.Packages.harvested }} packages harvested,
    {{ .Summary.Packages.restricted }} restricted,
    {{ .Summary.Packages.excluded }} withheld
  </p>

  <p class="claim">
    This atlas asserts that these primitives were published at these sources, at
    these resolved commit SHAs, read at the time above, by a principal with this
    much access. It does <strong>not assert</strong> that anything was approved,
    reviewed, unaltered, or authorised to run.
  </p>

  {{ if .Collisions }}
  <div class="notice">
    <strong>Name collisions ({{ len .Collisions }}).</strong>
    A package name is only meaningful relative to its source, so the same name can
    legitimately appear in more than one. Both are listed; nothing is hidden.
    <ul>
      {{ range .Collisions }}
      <li><span class="mono">{{ .Name }}</span> — {{ .Kind }}, in
        {{ range $i, $s := .Sources }}{{ if $i }}, {{ end }}<span class="mono">{{ $s }}</span>{{ end }}
      </li>
      {{ end }}
    </ul>
  </div>
  {{ end }}

  {{ range $src := .Sources }}
  <h2>{{ $src.Name }} <span class="tag">{{ $src.Kind }}</span></h2>

  {{ if eq $src.Status "unavailable" }}
    <div class="card unavailable">
      <p class="desc">Source <span class="mono">{{ $src.Name }}</span> could not be
      read — its packages are not listed.</p>
      <p class="meta">Reason: {{ $src.Reason }}</p>
    </div>
  {{ else }}
    {{ if $src.Owner }}<p class="meta">Owner {{ $src.Owner }}{{ if $src.Version }} · version {{ $src.Version }}{{ end }}</p>{{ end }}
    {{ range $pkg := $.Packages }}{{ if eq $pkg.Source $src.Name }}
      <div class="card{{ if ne $pkg.Access "public" }} withheld{{ end }}">
        <h3>{{ $pkg.Name }}
          {{ if eq $pkg.Access "excluded" }}<span class="tag">withheld by descriptor</span>{{ end }}
          {{ if eq $pkg.Access "restricted" }}<span class="tag">access restricted</span>{{ end }}
          {{ if $pkg.Version }}<span class="tag">{{ $pkg.Version }}</span>{{ end }}
        </h3>
        {{ if $pkg.Description }}<p class="desc">{{ $pkg.Description }}</p>{{ end }}

        {{ if eq $pkg.Access "public" }}
          {{ if $pkg.ResolvedSha }}<p class="meta mono">{{ $pkg.ResolvedSha }}</p>{{ end }}
          {{ if $pkg.Primitives }}
            <ul class="prims">
              {{ range $pkg.Primitives }}
              <li><span class="ptype">{{ .Type }}</span><span class="mono">{{ .Name }}</span>
                {{ if .Description }}<div class="meta">{{ .Description }}</div>{{ end }}
              </li>
              {{ end }}
            </ul>
          {{ else }}
            <p class="meta">No primitives found in this package.</p>
          {{ end }}
          {{ if $pkg.Install }}
<pre>{{ $pkg.Install.MarketplaceAdd }}
{{ $pkg.Install.Install }}</pre>
          {{ end }}
        {{ else }}
          <p class="meta">Primitives not listed{{ if $pkg.Reason }} — {{ $pkg.Reason }}{{ end }}.</p>
        {{ end }}
      </div>
    {{ end }}{{ end }}
  {{ end }}
  {{ end }}
</main>
</body>
</html>
```

- [ ] **Step 4: Write the renderer**

```go
// Package render turns an atlas into a self-contained HTML page.
//
// html/template escapes every interpolation by default, which is the injection
// guard for third-party frontmatter text. Never switch to text/template.
package render

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"

	"github.com/supermodular/atlas/internal/model"
)

//go:embed page.gohtml
var files embed.FS

// Render produces the complete page. No external requests: all CSS is inline
// and there is no script, so a generated atlas opens from disk unchanged.
func Render(a *model.Atlas) ([]byte, error) {
	t, err := template.ParseFS(files, "page.gohtml")
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, a); err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}
	return buf.Bytes(), nil
}
```

- [ ] **Step 5: Run to verify they pass**

Run: `cd ~/workspace/atlas && go test ./internal/render/ -v`
Expected: PASS — all seven tests.

- [ ] **Step 6: Commit**

```bash
cd ~/workspace/atlas
git add internal/render/
git commit -m "feat(render): self-contained HTML page from an atlas

html/template escapes every interpolation by default — that is the injection
guard for third-party frontmatter, and a test proves script markup in a
description renders inert. No external requests, so a page opens from disk.

The page states the claim boundary in prose and distinguishes 'withheld by
descriptor' from 'access restricted': one is a deliberate withholding, the other
a limit on the reader's access, and collapsing them would overstate what the
atlas knows. A test bans phrases that would imply approval or tamper-evidence."
```

---

### Task 10: The CLI

**Files:**
- Create: `~/workspace/atlas/cmd/atlas/main.go`
- Test: `~/workspace/atlas/cmd/atlas/main_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces: the `atlas` binary — `--descriptor`, `--out`, `--strict`.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/supermodular/atlas/internal/gitc"
)

func TestRunWritesBothArtifacts(t *testing.T) {
	repo := gitc.NewFixtureRepo(t, map[string]string{
		".claude/skills/a/SKILL.md": "---\nname: a\ndescription: Does a thing.\n---\nbody",
	}, "")
	dir := t.TempDir()
	desc := filepath.Join(dir, "d.yml")
	if err := os.WriteFile(desc, []byte("company: acme\nsources:\n  - kind: repo\n    name: r\n    url: "+repo+"\n    acknowledgeUnclassified: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "site")

	if err := run(desc, out, false); err != nil {
		t.Fatalf("run: %v", err)
	}

	js, err := os.ReadFile(filepath.Join(out, "atlas.json"))
	if err != nil {
		t.Fatalf("atlas.json: %v", err)
	}
	if !strings.Contains(string(js), `"schemaVersion": 1`) {
		t.Error("atlas.json missing schemaVersion")
	}
	html, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatalf("index.html: %v", err)
	}
	if !strings.Contains(string(html), "Does a thing.") {
		t.Error("index.html missing harvested description")
	}
}

func TestStrictFailsOnDegradation(t *testing.T) {
	dir := t.TempDir()
	desc := filepath.Join(dir, "d.yml")
	if err := os.WriteFile(desc, []byte("company: acme\nsources:\n  - kind: marketplace\n    name: gone\n    url: file:///nonexistent-atlas-strict\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "site")

	if err := run(desc, out, false); err != nil {
		t.Fatalf("non-strict run should succeed: %v", err)
	}
	if err := run(desc, out, true); err == nil {
		t.Fatal("--strict must fail when a source is unavailable")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd ~/workspace/atlas && go test ./cmd/atlas/ -v`
Expected: FAIL — `undefined: run`.

- [ ] **Step 3: Add cobra and write the CLI**

```bash
cd ~/workspace/atlas
go get github.com/spf13/cobra@v1.10.2
```

```go
// Command atlas renders a company's published primitives into a static site.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/supermodular/atlas/internal/build"
	"github.com/supermodular/atlas/internal/descriptor"
	"github.com/supermodular/atlas/internal/render"
)

func main() {
	var (
		descPath string
		outDir   string
		strict   bool
	)
	root := &cobra.Command{
		Use:   "atlas",
		Short: "Render a company's published AI primitives into a static site",
		Long: "Atlas reads a company descriptor, harvests primitive metadata from published\n" +
			"marketplaces and plain repos, and writes atlas.json plus a self-contained\n" +
			"index.html.\n\n" +
			"Atlas is a reader: it never classifies, builds, or publishes.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(descPath, outDir, strict)
		},
	}
	root.Flags().StringVar(&descPath, "descriptor", "", "path to the company descriptor (required)")
	root.Flags().StringVar(&outDir, "out", "", "output directory (required)")
	root.Flags().BoolVar(&strict, "strict", false, "exit non-zero if any source or package degraded")
	_ = root.MarkFlagRequired("descriptor")
	_ = root.MarkFlagRequired("out")

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "atlas:", err)
		os.Exit(1)
	}
}

func run(descPath, outDir string, strict bool) error {
	d, err := descriptor.Load(descPath)
	if err != nil {
		return err
	}

	a, err := build.Build(build.Options{
		Descriptor: d,
		Now:        func() string { return time.Now().UTC().Format(time.RFC3339) },
	})
	if err != nil {
		return err
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create out dir: %w", err)
	}
	js, err := a.MarshalJSONIndent()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "atlas.json"), js, 0o644); err != nil {
		return fmt.Errorf("write atlas.json: %w", err)
	}
	html, err := render.Render(a)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "index.html"), html, 0o644); err != nil {
		return fmt.Errorf("write index.html: %w", err)
	}

	// Always report bounded coverage: silent truncation reads as completeness.
	fmt.Fprintf(os.Stderr, "%d sources: %d read, %d unavailable · %d packages: %d harvested, %d restricted, %d withheld\n",
		len(a.Sources), a.Summary.Sources["read"], a.Summary.Sources["unavailable"],
		len(a.Packages), a.Summary.Packages["harvested"],
		a.Summary.Packages["restricted"], a.Summary.Packages["excluded"])
	if len(a.Collisions) > 0 {
		fmt.Fprintf(os.Stderr, "%d name collision(s) reported on the page\n", len(a.Collisions))
	}

	if strict {
		degraded := a.Summary.Sources["unavailable"] + a.Summary.Packages["restricted"]
		if degraded > 0 {
			return fmt.Errorf("--strict: %d source(s)/package(s) degraded", degraded)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run to verify the tests pass**

Run: `cd ~/workspace/atlas && go test ./cmd/atlas/ -v && go build -o atlas ./cmd/atlas`
Expected: PASS, and a binary at `./atlas`.

- [ ] **Step 5: Commit**

```bash
cd ~/workspace/atlas
git add go.mod go.sum cmd/
git commit -m "feat(cli): atlas --descriptor --out [--strict]

Always prints the coverage summary to stderr, including what was withheld or
unreadable: silent truncation reads as 'covered everything' when it did not.
--strict turns any degradation into a non-zero exit for CI that must guarantee
completeness."
```

---

### Task 11: The no-hardcoded-org guard and the runnable example

**Files:**
- Create: `~/workspace/atlas/internal/guard/guard_test.go`
- Create: `~/workspace/atlas/examples/demo.yml`
- Create: `~/workspace/atlas/examples/fixture/` (a committed fixture marketplace)
- Create: `~/workspace/atlas/examples/README.md`
- Modify: `~/workspace/atlas/Makefile` (add `example` target)

**Interfaces:**
- Consumes: the built binary.
- Produces: guarantee test 3, and a demo that works with no private access.

- [ ] **Step 1: Write the failing guard test**

```go
// Package guard holds repo-wide invariants that are not tied to one package.
package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Guarantee test 3: Atlas is an OSS tool and must carry no company specifics.
// Everything company-shaped arrives via the descriptor or a fetched manifest.
func TestNoHardcodedOrgStrings(t *testing.T) {
	banned := []string{
		"smos-", "supermodularai", "supermodular-os",
		"playgrounds/personal", "transformation-stack", "ai-primitives",
		"joni.oliveira",
	}
	roots := []string{"../../internal", "../../cmd"}

	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, ".gohtml") {
				return nil
			}
			// This file necessarily contains the banned strings as data.
			if strings.HasSuffix(p, "guard_test.go") {
				return nil
			}
			body, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			lower := strings.ToLower(string(body))
			for _, b := range banned {
				if strings.Contains(lower, b) {
					t.Errorf("%s contains hardcoded org string %q — it must come from the descriptor or manifest", p, b)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}
```

- [ ] **Step 2: Run it**

Run: `cd ~/workspace/atlas && go test ./internal/guard/ -v`
Expected: PASS. If it FAILS, a previous task leaked an org string — fix that
task's file, do not weaken this test.

- [ ] **Step 3: Build the fixture marketplace**

The fixture must work for a stranger with no access to anything private, and it
doubles as the demo. Package sources are **relative paths**, resolved against a
`sourceBase` the example sets to the fixture directory itself.

```bash
cd ~/workspace/atlas/examples
mkdir -p fixture/marketplace fixture/pkg-demo/skills/hello-atlas fixture/pkg-demo/hooks

cat > fixture/pkg-demo/skills/hello-atlas/SKILL.md <<'SKILL'
---
name: hello-atlas
description: A demonstration skill, so the example renders something real.
---

# hello-atlas

This skill exists to give the Atlas example a primitive to find.
SKILL

cat > fixture/pkg-demo/hooks/example-guard.sh <<'HOOK'
#!/bin/sh
# A demonstration hook. Hooks carry no frontmatter, so Atlas lists the filename.
exit 0
HOOK
chmod +x fixture/pkg-demo/hooks/example-guard.sh
```

- [ ] **Step 4: Write the example script that makes the fixture into git repos**

Package sources must be clonable, so the example turns the fixture dirs into real
repos in a temp dir at run time. This keeps the committed fixture plain files.

Create `examples/run-example.sh`:

```bash
#!/usr/bin/env bash
# Builds the committed fixture into throwaway git repos, then renders an atlas
# from them. Requires only git and the atlas binary — no network, no access to
# anything private.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

mkrepo() {
  local src="$1" dest="$2"
  mkdir -p "$dest"
  cp -R "$src/." "$dest/"
  git -C "$dest" init -q -b main
  git -C "$dest" add -A
  GIT_AUTHOR_NAME=example GIT_AUTHOR_EMAIL=example@example.test \
  GIT_COMMITTER_NAME=example GIT_COMMITTER_EMAIL=example@example.test \
    git -C "$dest" commit -q -m fixture --no-gpg-sign
}

mkrepo "$here/fixture/pkg-demo" "$work/pkg-demo"

mkdir -p "$work/marketplace"
cat > "$work/marketplace/apm.yml" <<YAML
name: example-marketplace
version: 1.0.0
description: A fixture marketplace for the Atlas example.
marketplace:
  owner:
    name: example-co
  sourceBase: file://$work
  packages:
    - name: pkg-demo
      description: "A demonstration package containing one skill and one hook."
      source: pkg-demo
    - name: pkg-withheld
      description: "Listed by the marketplace, withheld by the descriptor."
      source: pkg-demo
YAML
mkrepo "$work/marketplace" "$work/marketplace-repo"

cat > "$work/demo.yml" <<YAML
company: example-co
sources:
  - kind: marketplace
    name: example
    url: file://$work/marketplace-repo
    exclude:
      - pkg-withheld
YAML

"$root/atlas" --descriptor "$work/demo.yml" --out "$root/dist/example"
echo "Rendered → $root/dist/example/index.html"
```

```bash
chmod +x ~/workspace/atlas/examples/run-example.sh
```

- [ ] **Step 5: Write `examples/README.md`**

```markdown
# Example

A self-contained fixture marketplace. Needs only `git` and the `atlas` binary —
no network, no access to any private repo.

```bash
make build
./examples/run-example.sh
open dist/example/index.html
```

It demonstrates three states on one page:

- **`pkg-demo`** — harvested, showing one skill (with a description read from
  frontmatter) and one hook (filename only, since hooks carry no frontmatter).
- **`pkg-withheld`** — listed by the marketplace, excluded by the descriptor.
  Its card renders with the manifest's name and description and its interior
  withheld, which is what an ACL-gated or confidential package looks like.
- The **claim boundary** paragraph, stating what the atlas does and does not
  assert.
```

- [ ] **Step 6: Add the Makefile target**

Replace the Makefile with:

```makefile
.PHONY: test build lint example all
all: lint test build

test:
	go test ./...

build:
	go build -o atlas ./cmd/atlas

lint:
	go vet ./...

example: build
	./examples/run-example.sh
```

- [ ] **Step 7: Run the example end to end**

Run: `cd ~/workspace/atlas && make example`
Expected: the summary line on stderr showing 1 source read, 1 package harvested
and 1 withheld, then `Rendered → .../dist/example/index.html`.

Open the page and confirm by eye: the withheld card shows a description but no
primitives, and the harvested card lists `hello-atlas` and `example-guard.sh`.

- [ ] **Step 8: Run the whole suite and commit**

```bash
cd ~/workspace/atlas
make all
git add internal/guard/ examples/ Makefile
git commit -m "test(guard): ban hardcoded org strings; add a runnable fixture example

The guard is guarantee test 3: Atlas is public, so no company specifics may live
in internal/ or cmd/. Everything company-shaped arrives via the descriptor or a
fetched manifest.

The example is the artifact that lets a stranger run Atlas with no access to
anything private, and it exercises the withheld-card path — the one that matters
most and would otherwise only be covered by unit tests."
```

---

### Task 12: Run it against the real marketplace

**Files:**
- Create: `~/workspace/atlas/docs/first-run.md`

**Interfaces:**
- Consumes: the built binary.
- Produces: evidence Atlas works on real input, and a record of what it found.

This task is deliberately last: everything before it is verifiable offline, so a
failure here isolates to the real-world input rather than to Atlas.

- [ ] **Step 1: Write the real descriptor OUTSIDE the repo**

The descriptor names a private namespace, so it must not be committed to a
repo destined for GitHub (spec §3: descriptors live in the company's own repo).

```bash
mkdir -p ~/workspace/atlas-local
cat > ~/workspace/atlas-local/supermodular.yml <<'YAML'
company: supermodular
sources:
  - kind: marketplace
    name: ai-primitives
    url: https://gitlab.com/supermodularai/core/transformation-stack/ai-primitives/marketplace
    exclude:
      - smos-finance
      - smos-access
YAML
```

- [ ] **Step 2: Run Atlas against it**

```bash
cd ~/workspace/atlas
./atlas --descriptor ~/workspace/atlas-local/supermodular.yml --out ~/workspace/atlas-local/site
```

Expected: a summary line reporting 1 source read, 7 packages harvested and 2
withheld. If a package comes back `restricted`, that is a real access finding —
record it in Step 4 rather than working around it.

- [ ] **Step 3: Verify the two things that matter**

```bash
cd ~/workspace/atlas-local
# No excluded content leaked, by name or description:
grep -c "finance-ops\|externals-management\|finance-auditor" site/atlas.json || echo "OK: no excluded primitive names"
# The excluded packages still appear as withheld cards:
grep -o '"name": "smos-\(finance\|access\)"' site/atlas.json
# Provenance recorded, including the namespace actually resolved:
grep -o '"resolvedFrom": "[^"]*"' site/atlas.json | head -3
```

Expected: the first command prints `OK: no excluded primitive names`; the second
lists both packages; the third shows the `sourceBase` from the published
manifest.

- [ ] **Step 4: Write `docs/first-run.md` recording what was found**

Include: the date, the package count by state, the `resolvedFrom` prefix actually
used, and — if the published `sourceBase` still points at the vacated
`playgrounds/personal` namespace — the observation that Atlas recorded resolution
via a redirect. That discrepancy is the emitter's hardcoded constant, not an Atlas
defect, and recording it is the point of `resolvedFrom`.

Do **not** paste any excluded package's contents into this file.

- [ ] **Step 5: Commit**

```bash
cd ~/workspace/atlas
git add docs/first-run.md
git commit -m "docs: first run against a real marketplace

Records what Atlas found on real input: package counts by state, the namespace
actually resolved, and whether the published sourceBase still resolves via a
GitLab redirect. The descriptor itself is deliberately not committed — it names a
private namespace, and descriptors belong in the company's own repo."
```

---

## Self-Review

**1. Spec coverage**

| Spec section | Task |
|---|---|
| §1 separate project | Task 1 |
| §2 scope, MIT, host-agnostic | Tasks 1, 4 |
| §3 descriptor, two kinds, excludes both kinds | Tasks 2, 8 |
| §3 repo-mode fail-closed | Task 8 (`buildRepo`), test 6 |
| §4 four-stage pipeline | Tasks 7 (resolve), 5–6 (harvest), 8 (merge), 9 (render) |
| §4 reproducibility via recorded SHA | Task 4, Task 7 `ResolveRef` |
| §5 atlas.json schema, null vs [] | Task 3 |
| §5 schemaVersion | Task 3 |
| §6 provenance + both collision kinds | Task 8 |
| §7 two-level degradation | Task 8, Task 9 (rendering), Task 10 (`--strict`) |
| §8 install commands, qualified, omitted when unknown | Task 8, Task 9 |
| §9 claim boundary in output | Task 9 (two tests) |
| §10 self-contained, escaped, no renderer inherited | Task 9 |
| §11 layout, exact pins, demo fixture | Tasks 1, 11 |
| §12 all eight guarantee tests | 1→T8, 2→T8, 3→T11, 4→T9, 5→T6/T8, 6→T8, 7→see gap below, 8→T8 |
| §13 portability | Task 12 (proves descriptor-only reuse) |
| §14 verified live target | Task 12 |

**Known gap, deliberate:** guarantee test 7 (`--audience company` omits
personal-tier primitives) is **not** implemented. `--audience` requires reading a
classification file's audience values, and Task 8 only *detects* whether such a
file exists. This is a genuine scope reduction from spec §3 mechanism 1, which
says Atlas should obey `confidential` **and** an audience ceiling. What ships
obeys `confidential`-by-descriptor-exclusion and fails closed on unclassified
repos; the audience ceiling is deferred. **Add it as a follow-up task before
using Atlas on a repo whose safety depends on audience tiers rather than on
excludes.** Recorded here rather than silently dropped.

**2. Placeholder scan:** no TBD/TODO; every code step carries complete code;
every test step names the exact command and expected result.

**3. Type consistency:** `model.Package.Primitives` is `[]model.Primitive`
throughout (nil-vs-empty preserved in Tasks 3, 6, 8, 9). `gitc.Clone` returns
`*CloneResult` with `Dir`/`Sha`, consumed as such in Task 8.
`descriptor.Source.IsExcluded` takes one string and is called with a package name
(marketplace) and a rel path (repo) — matching Task 2's two branches.
`harvest.WalkTree(root, WalkOptions)` matches its Task 8 call sites.
`resolve.Manifest.ResolveURL/ResolveRef` take `ManifestPackage`, as called.
`render.Render(*model.Atlas)` matches Task 10.
