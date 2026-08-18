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
