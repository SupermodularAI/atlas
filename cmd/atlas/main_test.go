package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesBothArtifacts(t *testing.T) {
	repo := newFixtureRepo(t, map[string]string{
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

// TestStrictSucceedsOnCleanRun pins the other half of the --strict contract:
// a run with zero unavailable sources, zero restricted packages, and zero
// warnings must succeed under --strict, not merely fail correctly when
// degraded. Without this case, replacing the --strict condition with an
// unconditional failure (every run fails, degraded or not) still passes
// TestStrictFailsOnDegradation and TestStrictFailsOnWarningWithNoDegradation,
// since both of those only assert failure on an already-degraded fixture.
//
// The fixture must be genuinely clean: no exclude entries at all, since an
// exclude that matches nothing emits its own unused-exclude warning and would
// make the run non-clean.
func TestStrictSucceedsOnCleanRun(t *testing.T) {
	repo := newFixtureRepo(t, map[string]string{
		".claude/skills/a/SKILL.md": "---\nname: a\ndescription: Does a thing.\n---\nbody",
	}, "")
	dir := t.TempDir()
	desc := filepath.Join(dir, "d.yml")
	if err := os.WriteFile(desc, []byte("company: acme\nsources:\n  - kind: repo\n    name: r\n    url: "+repo+"\n    acknowledgeUnclassified: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "site")

	if err := run(desc, out, true); err != nil {
		t.Fatalf("--strict must succeed on a clean run (zero unavailable, zero restricted, zero warnings): %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "atlas.json")); err != nil {
		t.Errorf("atlas.json not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "index.html")); err != nil {
		t.Errorf("index.html not written: %v", err)
	}
}

// TestStrictFailsOnWarningWithNoDegradation pins the decision that --strict
// also fails on a recorded warning (e.g. an inert exclude pattern) even when
// every source was read and every package harvested — i.e. zero
// unavailable/restricted counters. Without this case, a --strict that only
// checks unavailable/restricted (ignoring warnings entirely) would still pass
// TestStrictFailsOnDegradation, since that test's fixture has no warnings.
func TestStrictFailsOnWarningWithNoDegradation(t *testing.T) {
	repo := newFixtureRepo(t, map[string]string{
		".claude/skills/a/SKILL.md": "---\nname: a\ndescription: Does a thing.\n---\nbody",
	}, "")
	dir := t.TempDir()
	desc := filepath.Join(dir, "d.yml")
	// exclude pattern matches nothing in the fixture tree -> unused-exclude
	// warning, but the source is still fully read and the package still
	// harvested: no unavailable source, no restricted package.
	if err := os.WriteFile(desc, []byte("company: acme\nsources:\n  - kind: repo\n    name: r\n    url: "+repo+"\n    exclude:\n      - \"nothing-matches-this/*\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "site")

	if err := run(desc, out, false); err != nil {
		t.Fatalf("non-strict run should succeed despite the warning: %v", err)
	}
	if err := run(desc, out, true); err == nil {
		t.Fatal("--strict must fail when a warning was recorded, even with no unavailable/restricted counts")
	}
}
