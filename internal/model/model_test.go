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

// TestInstallOmitsUnpopulatedCommands pins the blocking fix: a command that
// was not constructed (no sourceBase, or a repo source with no install path)
// must be omitted, never emitted as "". An empty string is worse than a
// missing field — it invites a consumer to run a command that does not exist.
func TestInstallOmitsUnpopulatedCommands(t *testing.T) {
	partial := Install{MarketplaceAdd: "apm marketplace add u --name s"}
	pb, err := json.Marshal(partial)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(pb), `"install"`) {
		t.Errorf("unpopulated Install field must be omitted, got %s", pb)
	}
	if !strings.Contains(string(pb), `"marketplaceAdd":"apm marketplace add u --name s"`) {
		t.Errorf("populated marketplaceAdd must survive marshalling: %s", pb)
	}

	full := Install{
		MarketplaceAdd: "apm marketplace add u --name s",
		Install:        "apm install p@s --target claude",
	}
	fb, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fb), `"marketplaceAdd":"apm marketplace add u --name s"`) {
		t.Errorf("marketplaceAdd missing from fully-populated Install: %s", fb)
	}
	if !strings.Contains(string(fb), `"install":"apm install p@s --target claude"`) {
		t.Errorf("install missing from fully-populated Install: %s", fb)
	}
}

// TestExportedConstantLiteralValues pins the exact wire value of every
// exported constant. atlas.json is a public schema — a typo in a constant
// that no existing test happens to reference would otherwise ship green.
func TestExportedConstantLiteralValues(t *testing.T) {
	cases := map[string]string{
		"AccessPublic":      AccessPublic,
		"AccessRestricted":  AccessRestricted,
		"AccessExcluded":    AccessExcluded,
		"StatusRead":        StatusRead,
		"StatusUnavailable": StatusUnavailable,
		"TypeSkill":         TypeSkill,
		"TypeAgent":         TypeAgent,
		"TypeHook":          TypeHook,
		"TypeCommand":       TypeCommand,
		"TypeMCP":           TypeMCP,
	}
	want := map[string]string{
		"AccessPublic":      "public",
		"AccessRestricted":  "restricted",
		"AccessExcluded":    "excluded",
		"StatusRead":        "read",
		"StatusUnavailable": "unavailable",
		"TypeSkill":         "skill",
		"TypeAgent":         "agent",
		"TypeHook":          "hook",
		"TypeCommand":       "command",
		"TypeMCP":           "mcp",
	}
	for name, got := range cases {
		if got != want[name] {
			t.Errorf("%s = %q, want %q", name, got, want[name])
		}
	}
	if SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", SchemaVersion)
	}
}

// TestAtlasWireTagNames pins every JSON key on a fully-populated Atlas. For a
// public schema a renamed or typo'd wire tag is exactly as breaking as the
// null-vs-empty bug already tested for.
func TestAtlasWireTagNames(t *testing.T) {
	a := &Atlas{
		SchemaVersion: SchemaVersion,
		Company:       "acme",
		GeneratedAt:   "2026-08-18T11:20:00Z",
		Sources: []Source{
			{
				Name:       "example-marketplace",
				Kind:       "marketplace",
				Status:     StatusRead,
				SourceBase: "https://example.com/org",
				Owner:      "acme",
				Version:    "0.2.1",
			},
		},
		Packages: []Package{
			{
				Name:         "widget-infra",
				Source:       "example-marketplace",
				Description:  "desc",
				Version:      "0.2.1",
				ResolvedFrom: "https://example.com/org/widget-infra",
				ResolvedSha:  "99bbbb8d952b80882ce5a68fc588580f8f16756b",
				Access:       AccessPublic,
				Primitives: []Primitive{
					{Type: TypeSkill, Name: "review-helper", Description: "reviews MRs"},
				},
				Install: &Install{
					MarketplaceAdd: "apm marketplace add u --name example-marketplace",
					Install:        "apm install widget-infra@example-marketplace --target claude",
				},
			},
		},
		Collisions: []Collision{
			{Kind: "package-name", Name: "widget-infra", Sources: []string{"widget", "core"}},
		},
		Summary: Summary{
			Sources:  map[string]int{"read": 1},
			Packages: map[string]int{"harvested": 1},
		},
		Warnings: []Warning{
			{Kind: "unused-exclude", Source: "example-marketplace", Detail: "pattern matched nothing"},
		},
	}

	b, err := a.MarshalJSONIndent()
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)

	wantKeys := []string{
		`"schemaVersion"`,
		`"company"`,
		`"generatedAt"`,
		`"sources"`,
		`"packages"`,
		`"collisions"`,
		`"summary"`,
		`"warnings"`,
		`"name"`,
		`"kind"`,
		`"status"`,
		`"sourceBase"`,
		`"owner"`,
		`"version"`,
		`"source"`,
		`"description"`,
		`"resolvedFrom"`,
		`"resolvedSha"`,
		`"access"`,
		`"primitives"`,
		`"type"`,
		`"install"`,
		`"marketplaceAdd"`,
		`"detail"`,
	}
	for _, key := range wantKeys {
		if !strings.Contains(s, key) {
			t.Errorf("wire tag %s missing from fully-populated Atlas: %s", key, s)
		}
	}
}

// TestWarningsEmitsEmptyArrayNotNull mirrors Collisions: a consumer must be
// able to distinguish "no warnings" from "this producer does not populate
// this field", so an empty Warnings slice must marshal to [] not null.
func TestWarningsEmitsEmptyArrayNotNull(t *testing.T) {
	a := &Atlas{SchemaVersion: SchemaVersion, Warnings: []Warning{}}
	b, err := a.MarshalJSONIndent()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"warnings": []`) {
		t.Errorf("empty Warnings must emit warnings: [], got %s", b)
	}
}
