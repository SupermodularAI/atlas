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

func TestRejectsMalformedExcludeGlob(t *testing.T) {
	_, err := Load(write(t, `
company: acme
sources:
  - kind: repo
    name: r
    url: https://example.test/r
    acknowledgeUnclassified: true
    exclude:
      - "skills/finance-[ops/**"
`))
	if err == nil {
		t.Fatal("expected error for malformed exclude glob, got nil")
	}
}

func TestAcknowledgeUnclassifiedRoundTrips(t *testing.T) {
	d, err := Load(write(t, `
company: acme
sources:
  - kind: repo
    name: r
    url: https://example.test/r
    acknowledgeUnclassified: true
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !d.Sources[0].AcknowledgeUnclassified {
		t.Error("AcknowledgeUnclassified = false, want true")
	}
}

func TestRejectsNonTrailingDoubleStar(t *testing.T) {
	_, err := Load(write(t, `
company: acme
sources:
  - kind: repo
    name: r
    url: https://example.test/r
    acknowledgeUnclassified: true
    exclude:
      - "skills/**/SKILL.md"
`))
	if err == nil {
		t.Fatal("expected error for non-trailing ** in exclude glob, got nil")
	}
}

func TestRejectsMalformedExcludeGlobWithoutSuffix(t *testing.T) {
	_, err := Load(write(t, `
company: acme
sources:
  - kind: repo
    name: r
    url: https://example.test/r
    acknowledgeUnclassified: true
    exclude:
      - "skills/finance-[ops"
`))
	if err == nil {
		t.Fatal("expected error for malformed exclude glob without /** suffix, got nil")
	}
}

func TestRepoSourceGlobExcludeWithoutSuffixStillExcludesBeneath(t *testing.T) {
	// An author writing "skills/finance-*" (no "/**" suffix) means "withhold
	// the finance skills" — the same intent as the "/**" form. Silently
	// excluding nothing beneath the directory would be the same fail-open
	// trap as the main finding, reached via an authoring gap instead of a
	// code defect.
	d, err := Load(write(t, `
company: acme
sources:
  - kind: repo
    name: r
    url: https://example.test/r
    exclude:
      - "skills/finance-*"
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := d.Sources[0]
	if !s.IsExcluded("skills/finance-ops/SKILL.md") {
		t.Error("finance-ops path should be excluded even without a /** suffix")
	}
	if !s.IsExcluded("skills/finance-ops/a/b/SKILL.md") {
		t.Error("deep finance-ops path should be excluded even without a /** suffix")
	}
	if s.IsExcluded("skills/code-review/SKILL.md") {
		t.Error("code-review path should not be excluded")
	}
}

func TestRejectsMalformedExcludeGlobAfterLiteralPrefix(t *testing.T) {
	// A malformed character class positioned after a literal prefix segment
	// that would otherwise fail the match early: this is the case where a
	// naive check (e.g. path.Match(pat, "") ) could short-circuit on the
	// name mismatch before ever scanning the unterminated bracket. Self-
	// matching the pattern against itself forces the whole pattern to be
	// scanned for well-formedness.
	_, err := Load(write(t, `
company: acme
sources:
  - kind: repo
    name: r
    url: https://example.test/r
    acknowledgeUnclassified: true
    exclude:
      - "zzz/[bad"
`))
	if err == nil {
		t.Fatal("expected error for malformed exclude glob after literal prefix, got nil")
	}
}

func TestRejectsExcludePatternWithNoMatchableContent(t *testing.T) {
	// Each of these is well-formed (compiles as a path.Match pattern) and
	// distinct from the rejected "**" form, yet after trimming a trailing
	// "/**" leaves nothing that can match a real path — so it is accepted
	// at load and then silently excludes nothing. checkExcludePattern must
	// reject these on the "no matchable content" ground, not by blacklist.
	for _, pat := range []string{"/**", "", "/", "."} {
		t.Run(pat, func(t *testing.T) {
			_, err := Load(write(t, `
company: acme
sources:
  - kind: repo
    name: r
    url: https://example.test/r
    acknowledgeUnclassified: true
    exclude:
      - "`+pat+`"
`))
			if err == nil {
				t.Fatalf("pattern %q: expected error for no matchable content, got nil", pat)
			}
		})
	}
}

func TestMatchGlobFailsClosedOnUncompilablePattern(t *testing.T) {
	// validate() guarantees every pattern reaching matchGlob compiles, so
	// this path is unreachable through Load. It is still reachable by any
	// caller that constructs a Source directly (bypassing Load), which is
	// exactly why matchGlob has a fail-closed backstop for a path.Match
	// error: construct that case directly and confirm the backstop excludes
	// rather than admits.
	s := Source{
		Kind:    KindRepo,
		Name:    "r",
		URL:     "https://example.test/r",
		Exclude: []string{"[bad"},
	}
	if !s.IsExcluded("anything") {
		t.Error("uncompilable pattern should fail closed (excluded = true), not admit")
	}
}

func TestRepoSourceGlobExcludeDeeperPath(t *testing.T) {
	// This is the case that discriminates the ancestor-walk implementation
	// from a naive single path.Match call: path.Match requires an exact
	// segment-count match, so only the ancestor walk in matchGlob can match
	// a path more than one level below the excluded prefix.
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
	if !s.IsExcluded("skills/finance-ops/a/b/SKILL.md") {
		t.Error("deep finance-ops path should be excluded")
	}
}
