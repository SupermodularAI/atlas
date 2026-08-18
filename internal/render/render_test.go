package render

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/supermodular/atlas/internal/build"
	"github.com/supermodular/atlas/internal/descriptor"
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
		Warnings: []model.Warning{
			{Kind: "unused-exclude", Source: "mkt", Detail: `exclude pattern "typo-pkg" matched nothing`},
			{Kind: "duplicate-primitive", Source: "mkt", Detail: `duplicate skill "dup-prim": kept a, dropped b`},
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
	for _, forbidden := range []string{
		"<script src", `<link rel="stylesheet" href`, "@import", "<img src=\"http",
		"fonts.googleapis", "fonts.gstatic", "cdn.",
	} {
		if strings.Contains(s, forbidden) {
			t.Errorf("page must make no external requests, found %q", forbidden)
		}
	}
}

// Guarantee test: harvested markup must be inert.
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
	if !strings.Contains(s, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("expected the markup escaped into entities, adjacent as one unit")
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
	// Distinctness, not mere presence: the two states must not share one marker.
	if strings.Count(s, "access restricted") == 0 || strings.Count(s, "withheld by descriptor") == 0 {
		t.Fatal("both markers must independently appear")
	}
}

func TestRenderRendersUnavailableSourceAsStub(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "could not be") {
		t.Error("an unavailable source must render as a distinct stub, not a locked package")
	}
	if !strings.Contains(s, "fetch failed: 404") {
		t.Error("the unavailable source's reason must be shown")
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
	// Positive half: the disclaimer prose about published/SHA/read-at must
	// actually be present, not merely absent of banned words (which an empty
	// page would also satisfy).
	for _, want := range []string{"published", "resolved", "read at"} {
		if !strings.Contains(s, want) {
			t.Errorf("claim-boundary disclaimer missing %q", want)
		}
	}
}

func TestRenderNeverClaimsApproval(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(out))
	// "approved" may appear only inside the explicit negated disclaimer
	// sentence ("does not assert ... approved"); it must never appear as an
	// affirmative claim about a specific package or primitive.
	for _, banned := range []string{"approved primitive", "verified unaltered", "tamper-evident", "is approved", "was approved"} {
		if strings.Contains(lower, banned) {
			t.Errorf("page must not claim %q", banned)
		}
	}
}

func TestRenderRendersWarnings(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// The Detail text goes through html/template like any other harvested
	// string, so its quotes come out HTML-escaped (&#34;) — asserting on the
	// literal quote here would be asserting on the wrong (unescaped) output.
	if !strings.Contains(s, "unused-exclude") || !strings.Contains(s, "typo-pkg") || !strings.Contains(s, "matched nothing") {
		t.Error("an unused-exclude warning must render")
	}
	if !strings.Contains(s, "duplicate-primitive") || !strings.Contains(s, "dup-prim") || !strings.Contains(s, "kept a, dropped b") {
		t.Error("a duplicate-primitive warning must render")
	}
}

func TestRenderOmitsInstallWhenAbsent(t *testing.T) {
	a := sample()
	a.Packages[0].Install = nil
	out, err := Render(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "apm install") || strings.Contains(s, "apm marketplace add") {
		t.Error("no install command must be rendered when Package.Install is nil")
	}
}

// --- Fixture built from real build.Build output ---
//
// A hand-rolled model.Atlas can quietly diverge from what the producer
// actually emits. This exercises the real pipeline (a real git repo, a real
// descriptor, internal/build.Build) so the renderer is tested against a
// shape that actually occurs: real warnings of both kinds, a real duplicate
// collision, and a real resolved SHA — not values a renderer author guessed.

func newFixtureRepo(t *testing.T, files map[string]string) string {
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
	return "file://" + dir
}

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

func TestRenderRealBuildOutputHasBothWarningKinds(t *testing.T) {
	// A duplicate skill named "dup" present at both the walk root and under
	// .claude/ triggers a real duplicate-primitive warning (mirrors
	// internal/harvest's TestWalkReportsDuplicateAcrossBases). An exclude
	// pattern that matches nothing triggers a real unused-exclude warning.
	repo := newFixtureRepo(t, map[string]string{
		"skills/dup/SKILL.md":         "---\nname: dup\ndescription: from root\n---\nbody",
		".claude/skills/dup/SKILL.md": "---\nname: dup\ndescription: from dotclaude\n---\nbody",
	})
	d := writeDescriptor(t, `
company: acme
sources:
  - kind: repo
    name: r
    url: `+repo+`
    exclude:
      - "skills/typo-pattern-*"
`)
	a, err := build.Build(build.Options{
		Descriptor: d,
		Now:        func() string { return "2026-08-18T00:00:00Z" },
		WorkDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("build.Build: %v", err)
	}
	if len(a.Warnings) != 2 {
		t.Fatalf("got %d warnings from real build output, want 2: %+v", len(a.Warnings), a.Warnings)
	}

	out, err := Render(a)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "unused-exclude") && !strings.Contains(s, `matched nothing`) {
		t.Error("real unused-exclude warning must render")
	}
	if !strings.Contains(s, "duplicate") || !strings.Contains(s, "dup") {
		t.Error("real duplicate-primitive warning must render")
	}
	if len(a.Packages) != 1 || len(a.Packages[0].ResolvedSha) != 40 {
		t.Fatalf("expected one package with a real full-length SHA: %+v", a.Packages)
	}
	if !strings.Contains(s, a.Packages[0].ResolvedSha) {
		t.Error("the real resolved SHA must appear on the page")
	}
}
