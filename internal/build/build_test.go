package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/supermodular/atlas/internal/descriptor"
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
	repo := newFixtureRepo(t, map[string]string{
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
	if p.Install != nil {
		t.Errorf("Install = %+v, want nil for a repo source (no install path)", p.Install)
	}
}

// Guarantee test 6: fail closed on an unclassified repo.
func TestBuildRefusesUnacknowledgedUnclassifiedRepo(t *testing.T) {
	repo := newFixtureRepo(t, map[string]string{
		".claude/skills/a/SKILL.md": "---\nname: a\ndescription: d\n---\nbody",
	}, "")
	d := writeDescriptor(t, `
company: acme
sources:
  - kind: repo
    name: unclassified-src
    url: `+repo+`
`)
	_, err := Build(Options{Descriptor: d, Now: fixedNow, WorkDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected refusal: no classification, no excludes, no acknowledgement")
	}
	// A weak `err != nil` assertion would also pass if descriptor.Load or the
	// clone itself failed for an unrelated reason. A distinctive source name
	// pins the refusal to the fail-closed path specifically — a short letter
	// like "r" would match almost any error text ("harvest", "error", ...).
	if !strings.Contains(err.Error(), "unclassified-src") {
		t.Errorf("error %q should name the offending source", err.Error())
	}
}

// Guarantee test 8, the critical one: exclusion must beat a SUCCESSFUL clone.
func TestBuildMarketplaceExcludeBeatsSuccessfulClone(t *testing.T) {
	secret := newFixtureRepo(t, map[string]string{
		"skills/finance-ops/SKILL.md": "---\nname: finance-ops\ndescription: SECRETVALUE.\n---\nbody",
	}, "v1.0.0")
	open := newFixtureRepo(t, map[string]string{
		"skills/code-review/SKILL.md": "---\nname: code-review\ndescription: Fine.\n---\nbody",
	}, "v1.0.0")

	// A manifest whose sources are absolute file:// URLs, both readable.
	mkt := newFixtureRepo(t, map[string]string{
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

	var secretPkg, openPkg *model.Package
	for i := range a.Packages {
		switch a.Packages[i].Name {
		case "pkg-secret":
			secretPkg = &a.Packages[i]
		case "pkg-open":
			openPkg = &a.Packages[i]
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

	// Control: pkg-open proves the fixture GRANTS access — both repos clone
	// cleanly. Without this, a broken/unreadable fixture could make G8 pass
	// for the wrong reason (a failed clone doing the withholding instead of
	// the descriptor's exclude rule).
	if openPkg == nil {
		t.Fatal("pkg-open must be present and harvested")
	}
	if openPkg.Access != model.AccessPublic {
		t.Errorf("pkg-open Access = %q, want public — proves the fixture is readable", openPkg.Access)
	}
	if len(openPkg.ResolvedSha) != 40 {
		t.Errorf("pkg-open ResolvedSha = %q, want a full SHA — proves the clone actually happened", openPkg.ResolvedSha)
	}
	if len(openPkg.Primitives) != 1 || openPkg.Primitives[0].Name != "code-review" {
		t.Errorf("pkg-open Primitives = %+v, want the harvested code-review skill", openPkg.Primitives)
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
	mkt := newFixtureRepo(t, map[string]string{
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
	if p.Install != nil {
		t.Errorf("Install = %+v, want nil on a restricted card", p.Install)
	}
	if a.Summary.Packages["restricted"] != 1 {
		t.Errorf("Summary.Packages = %+v", a.Summary.Packages)
	}
}

// A repo-mode exclude glob that matches nothing must surface as exactly one
// unused-exclude warning; a glob that did match must produce none. This is
// the visibility requirement docs/design.md §5 adds: a well-formed, accepted
// exclude pattern can still withhold nothing, and only a post-walk check can
// catch that.
func TestBuildRepoUnusedExcludeWarns(t *testing.T) {
	repo := newFixtureRepo(t, map[string]string{
		".claude/skills/code-review/SKILL.md": "---\nname: code-review\ndescription: Reviews code.\n---\nbody",
	}, "")
	d := writeDescriptor(t, `
company: acme
sources:
  - kind: repo
    name: r
    url: `+repo+`
    exclude:
      - "skills/typo-pattern-*"
`)
	a, err := Build(Options{Descriptor: d, Now: fixedNow, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(a.Warnings) != 1 {
		t.Fatalf("got %d warnings, want 1: %+v", len(a.Warnings), a.Warnings)
	}
	w := a.Warnings[0]
	if w.Kind != WarningUnusedExclude {
		t.Errorf("Kind = %q, want %q", w.Kind, WarningUnusedExclude)
	}
	if w.Source != "r" {
		t.Errorf("Source = %q, want %q", w.Source, "r")
	}
}

func TestBuildRepoMatchedExcludeWarnsNothing(t *testing.T) {
	// Root-layout paths (no .claude/ prefix): the exclude pattern is matched
	// against the walk-root-relative path, so it must share the same base as
	// the primitive it is meant to withhold.
	repo := newFixtureRepo(t, map[string]string{
		"skills/code-review/SKILL.md": "---\nname: code-review\ndescription: Reviews code.\n---\nbody",
		"skills/finance-ops/SKILL.md": "---\nname: finance-ops\ndescription: Money stuff.\n---\nbody",
	}, "")
	d := writeDescriptor(t, `
company: acme
sources:
  - kind: repo
    name: r
    url: `+repo+`
    exclude:
      - "skills/finance-*"
`)
	a, err := Build(Options{Descriptor: d, Now: fixedNow, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(a.Warnings) != 0 {
		t.Errorf("got %d warnings, want 0: %+v", len(a.Warnings), a.Warnings)
	}
	if len(a.Packages) != 1 || len(a.Packages[0].Primitives) != 1 {
		t.Fatalf("packages = %+v", a.Packages)
	}
	if a.Packages[0].Primitives[0].Name != "code-review" {
		t.Errorf("finance-ops should have been excluded, got %+v", a.Packages[0].Primitives)
	}
}

// A marketplace exclude pattern that matches no package name in the manifest
// must warn too — the same silent-ineffective-control failure, reached by
// the marketplace route rather than the repo-glob route.
func TestBuildMarketplaceUnusedExcludeWarns(t *testing.T) {
	open := newFixtureRepo(t, map[string]string{
		"skills/code-review/SKILL.md": "---\nname: code-review\ndescription: Fine.\n---\nbody",
	}, "")
	mkt := newFixtureRepo(t, map[string]string{
		"apm.yml": `
name: mkt
version: 1.0.0
marketplace:
  owner:
    name: acme
  packages:
    - name: pkg-open
      description: "Open."
      source: ` + open + `
`,
	}, "")
	d := writeDescriptor(t, `
company: acme
sources:
  - kind: marketplace
    name: mkt
    url: `+mkt+`
    exclude:
      - pkg-typo
`)
	a, err := Build(Options{Descriptor: d, Now: fixedNow, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(a.Warnings) != 1 || a.Warnings[0].Kind != WarningUnusedExclude {
		t.Fatalf("got %+v, want one unused-exclude warning", a.Warnings)
	}
}

// Non-obvious requirement: Build must initialise every slice field to a
// non-nil empty, never leave it nil, when there is genuinely nothing to
// report. A nil slice marshals to JSON null; the schema's producer
// obligation is "[]" for "looked and found none".
func TestBuildEmitsEmptyArraysNotNull(t *testing.T) {
	repo := newFixtureRepo(t, map[string]string{
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
	if a.Collisions == nil {
		t.Error("Collisions is nil, want non-nil empty slice")
	}
	if a.Warnings == nil {
		t.Error("Warnings is nil, want non-nil empty slice")
	}
	out := jsonOf(t, a)
	if strings.Contains(out, `"collisions": null`) {
		t.Error(`atlas.json emits "collisions": null, want "[]"`)
	}
	if strings.Contains(out, `"warnings": null`) {
		t.Error(`atlas.json emits "warnings": null, want "[]"`)
	}
	if !strings.Contains(out, `"collisions": []`) {
		t.Errorf("atlas.json should emit \"collisions\": [], got:\n%s", out)
	}
	if !strings.Contains(out, `"warnings": []`) {
		t.Errorf("atlas.json should emit \"warnings\": [], got:\n%s", out)
	}
}
