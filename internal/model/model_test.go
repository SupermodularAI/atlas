package model

import (
	"encoding/json"
	"slices"
	"sort"
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

// TestPackageWithZeroValueInstallEmitsEmptyObject documents a shape that is
// easy to introduce by accident now that both Install fields carry
// omitempty: a non-nil *Install pointing at a zero-valued Install marshals
// to "install":{} — present but empty. This is distinct from the nil-pointer
// case (correctly omitted by TestInstallOmitsUnpopulatedCommands's sibling,
// the Package-level omitempty on Install) and is not itself a bug, but a
// shape a future producer (ATLAS-08) must not introduce: when no install
// command can be derived, the pointer must stay nil, never a zero-valued
// &Install{}, or a consumer sees "install information exists" when none was
// derived.
func TestPackageWithZeroValueInstallEmitsEmptyObject(t *testing.T) {
	p := Package{Name: "a", Access: AccessPublic, Primitives: []Primitive{}, Install: &Install{}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"install":{}`) {
		t.Errorf(`Package{Install: &Install{}} must emit "install":{}, got %s`, b)
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

// wireKeys marshals v and returns the sorted set of JSON object keys at its
// top level only — it does not recurse into nested objects/arrays, since
// those are pinned by their own case in TestAtlasWireTagNames.
func wireKeys(t *testing.T, v any) []string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestAtlasWireTagNames pins the exact JSON key set of every struct in the
// atlas.json schema, one struct per case. Each value is fully populated
// (every field non-zero) so no key drops out via omitempty — an exact-set
// comparison must fail on a missing field, never on an incidentally-absent
// optional one. For a public schema, a renamed, typo'd, or spuriously added
// wire tag is exactly as breaking as the null-vs-empty bug already tested
// for; asserting the exact set (not containment) catches both a missing key
// and an extra one, and per struct (not per document) so one struct's tag
// can't hide behind another struct's tag of the same name.
func TestAtlasWireTagNames(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want []string
	}{
		{
			name: "Primitive",
			v:    Primitive{Type: TypeSkill, Name: "mr-review-agent", Description: "reviews MRs"},
			want: []string{"description", "name", "type"},
		},
		{
			name: "Install",
			v: Install{
				MarketplaceAdd: "apm marketplace add u --name ai-primitives",
				Install:        "apm install smos-infra@ai-primitives --target claude",
			},
			want: []string{"install", "marketplaceAdd"},
		},
		{
			name: "Package",
			v: Package{
				Name:         "smos-infra",
				Source:       "ai-primitives",
				Description:  "desc",
				Version:      "0.2.1",
				ResolvedFrom: "https://example.com/org/smos-infra",
				ResolvedSha:  "99bbbb8d952b80882ce5a68fc588580f8f16756b",
				Access:       AccessPublic,
				Reason:       "n/a",
				Primitives: []Primitive{
					{Type: TypeSkill, Name: "mr-review-agent", Description: "reviews MRs"},
				},
				Install: &Install{
					MarketplaceAdd: "apm marketplace add u --name ai-primitives",
					Install:        "apm install smos-infra@ai-primitives --target claude",
				},
			},
			want: []string{
				"access", "description", "install", "name", "primitives",
				"reason", "resolvedFrom", "resolvedSha", "source", "version",
			},
		},
		{
			name: "Source",
			v: Source{
				Name:       "ai-primitives",
				Kind:       "marketplace",
				Status:     StatusRead,
				SourceBase: "https://example.com/org",
				Owner:      "acme",
				Version:    "0.2.1",
				Reason:     "n/a",
			},
			want: []string{"kind", "name", "owner", "reason", "sourceBase", "status", "version"},
		},
		{
			name: "Collision",
			v:    Collision{Kind: "package-name", Name: "smos-infra", Sources: []string{"smos", "core"}},
			want: []string{"kind", "name", "sources"},
		},
		{
			name: "Warning",
			v:    Warning{Kind: "unused-exclude", Source: "ai-primitives", Detail: "pattern matched nothing"},
			want: []string{"detail", "kind", "source"},
		},
		{
			name: "Summary",
			v: Summary{
				Sources:  map[string]int{"read": 1},
				Packages: map[string]int{"harvested": 1},
			},
			want: []string{"packages", "sources"},
		},
		{
			name: "Atlas",
			v: &Atlas{
				SchemaVersion: SchemaVersion,
				Company:       "acme",
				GeneratedAt:   "2026-08-18T11:20:00Z",
				Sources: []Source{
					{Name: "ai-primitives", Kind: "marketplace", Status: StatusRead},
				},
				Packages: []Package{
					{Name: "smos-infra", Source: "ai-primitives", Access: AccessPublic},
				},
				Collisions: []Collision{
					{Kind: "package-name", Name: "smos-infra", Sources: []string{"smos", "core"}},
				},
				Summary: Summary{
					Sources:  map[string]int{"read": 1},
					Packages: map[string]int{"harvested": 1},
				},
				Warnings: []Warning{
					{Kind: "unused-exclude", Source: "ai-primitives", Detail: "pattern matched nothing"},
				},
			},
			want: []string{
				"collisions", "company", "generatedAt", "packages",
				"schemaVersion", "sources", "summary", "warnings",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := wireKeys(t, c.v)
			sort.Strings(c.want)
			if !slices.Equal(got, c.want) {
				t.Errorf("%s wire keys = %v, want %v", c.name, got, c.want)
			}
		})
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
