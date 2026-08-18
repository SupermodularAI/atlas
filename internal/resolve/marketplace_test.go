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
