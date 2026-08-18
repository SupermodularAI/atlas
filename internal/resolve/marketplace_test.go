package resolve

import (
	"errors"
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

// Finding 1: a document with no marketplace content at all must error, not
// silently parse to zero packages. Each case below has no `marketplace:` key
// present anywhere in the document.
//
// "empty", "null", and "just-comment" all decode cleanly to a zero-value
// document with no marketplace key — these must be rejected by the new
// presence check and name "marketplace" in the error. "whitespace" (mixed
// tabs/spaces/newlines with no valid YAML content) is rejected earlier, by
// yaml.Unmarshal itself as a syntax error — a different, but still correct,
// rejection of the same "no usable marketplace content" input.
func TestParseManifestRejectsNoMarketplaceContent(t *testing.T) {
	t.Run("empty", func(t *testing.T) { assertRejectsMissingMarketplace(t, "") })
	t.Run("null", func(t *testing.T) { assertRejectsMissingMarketplace(t, "null\n") })
	t.Run("just-comment", func(t *testing.T) { assertRejectsMissingMarketplace(t, "# just a comment\n") })

	t.Run("whitespace", func(t *testing.T) {
		_, err := ParseManifest([]byte("   \n\t\n"))
		if err == nil {
			t.Fatal("ParseManifest(whitespace) = nil error, want a rejection")
		}
	})
}

func assertRejectsMissingMarketplace(t *testing.T, input string) {
	t.Helper()
	_, err := ParseManifest([]byte(input))
	if err == nil {
		t.Fatalf("ParseManifest(%q) = nil error, want error naming missing marketplace content", input)
	}
	if !strings.Contains(err.Error(), "marketplace") {
		t.Errorf("err = %q, want it to mention the missing marketplace block", err.Error())
	}
}

// A present-but-empty packages list is a legitimate state (a real
// marketplace that currently publishes nothing) and must still parse.
func TestParseManifestAcceptsEmptyPackagesList(t *testing.T) {
	m, err := ParseManifest([]byte(`
name: ai-primitives
marketplace:
  owner:
    name: acme
  sourceBase: https://git.example.test/acme/group
`))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if len(m.Packages) != 0 {
		t.Errorf("len(Packages) = %d, want 0", len(m.Packages))
	}
	if m.Packages == nil {
		t.Errorf("Packages = nil, want non-nil empty slice for a present-but-empty marketplace.packages")
	}
}

// Finding 2: a URL-looking source containing "://" mid-string, not as a
// leading scheme, must not be passed through as-is.
func TestResolveURLRejectsMidStringScheme(t *testing.T) {
	m := &Manifest{SourceBase: "https://git.example.test/acme/group"}
	_, err := m.ResolveURL(ManifestPackage{Name: "p", Source: "weird://path"})
	if err == nil {
		t.Fatal("expected error: \"weird://path\" is not a recognised scheme")
	}
	if !strings.Contains(err.Error(), "p") {
		t.Errorf("err = %q, want it to name the package", err.Error())
	}
}

func TestResolveURLRejectsEmbeddedScheme(t *testing.T) {
	m := &Manifest{SourceBase: "https://git.example.test/acme/group"}
	_, err := m.ResolveURL(ManifestPackage{Name: "p", Source: "pkg-name-with-://-init"})
	if err == nil {
		t.Fatal("expected error: an embedded \"://\" must not be treated as a scheme")
	}
}

// scp-style sources (host:path, no "://") are outside the two source forms
// design.md §6 documents. Atlas rejects them explicitly rather than
// concatenating them into a garbage URL.
func TestResolveURLRejectsSCPStyleSource(t *testing.T) {
	m := &Manifest{SourceBase: "https://git.example.test/acme/group"}
	got, err := m.ResolveURL(ManifestPackage{Name: "p", Source: "gitlab.com:org/repo"})
	if err == nil {
		t.Fatalf("expected error for scp-style source, got url %q", got)
	}
	if !strings.Contains(err.Error(), "gitlab.com:org/repo") {
		t.Errorf("err = %q, want it to name the source", err.Error())
	}
}

func TestResolveURLAcceptsFileScheme(t *testing.T) {
	m := &Manifest{}
	got, err := m.ResolveURL(ManifestPackage{Name: "p", Source: "file:///tmp/some-fixture-repo"})
	if err != nil {
		t.Fatalf("ResolveURL: %v", err)
	}
	if got != "file:///tmp/some-fixture-repo" {
		t.Errorf("ResolveURL = %q", got)
	}
}

func TestResolveURLAcceptsGitAtPrefix(t *testing.T) {
	m := &Manifest{}
	got, err := m.ResolveURL(ManifestPackage{Name: "p", Source: "git@github.com:acme/repo.git"})
	if err != nil {
		t.Fatalf("ResolveURL: %v", err)
	}
	if got != "git@github.com:acme/repo.git" {
		t.Errorf("ResolveURL = %q", got)
	}
}

// Finding 3: a trailing slash (single or multiple) on sourceBase must not
// produce a doubled separator once ResolveURL concatenates it with source.
func TestResolveURLNoDoubledSeparatorWithTrailingSlashSourceBase(t *testing.T) {
	cases := map[string]string{
		"single slash":     "https://git.example.test/acme/group/",
		"multiple slashes": "https://git.example.test/acme/group///",
	}
	for name, sourceBase := range cases {
		t.Run(name, func(t *testing.T) {
			m, err := ParseManifest([]byte("marketplace:\n  sourceBase: " + sourceBase + "\n"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := m.ResolveURL(ManifestPackage{Name: "pkg-one", Source: "pkg-one"})
			if err != nil {
				t.Fatalf("ResolveURL: %v", err)
			}
			want := "https://git.example.test/acme/group/pkg-one"
			if got != want {
				t.Errorf("ResolveURL = %q, want %q", got, want)
			}
		})
	}
}

// Finding 4: the yaml.v3 parse error must not echo a fragment of the
// manifest's content into a rendered error string, per §9/§10.
func TestParseManifestErrorDoesNotEchoManifestContent(t *testing.T) {
	const secretLookingToken = "just-a-distinctive-token-should-not-leak"
	_, err := ParseManifest([]byte(secretLookingToken))
	if err == nil {
		t.Fatal("expected a parse error for a bare scalar document")
	}
	// yaml.v3 truncates a quoted fragment to a handful of characters (e.g.
	// "cannot unmarshal !!str `just-a-...`"), so asserting against the full
	// token would pass even if Error() forwarded the raw yaml message
	// verbatim. Assert against the truncated fragment yaml.v3 actually
	// quotes, so this test fails if Error() ever echoes it again.
	if err.Error() != "parse manifest: invalid YAML" {
		t.Errorf("err.Error() = %q, want the fixed content-free message", err.Error())
	}
	if strings.Contains(err.Error(), secretLookingToken[:7]) {
		t.Errorf("err = %q, must not quote a fragment of manifest content", err.Error())
	}
	underlying := errors.Unwrap(err)
	if underlying == nil {
		t.Fatal("expected errors.Unwrap to still reach the underlying yaml error")
	}
	if !strings.Contains(underlying.Error(), secretLookingToken[:7]) {
		t.Errorf("underlying.Error() = %q, want it to still mention the offending content for errors.Is/As callers", underlying.Error())
	}
}
