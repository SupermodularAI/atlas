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
//
// v2 renamed two primitive type values to the shared workstream vocabulary
// (ADR-0010): "agent" became "subagent" and "mcp" became "mcp_server". A
// consumer that matched on the v1 strings sees no subagents and no MCP servers,
// which is silent rather than loud — hence the bump.
const SchemaVersion = 2

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

// Primitive types. The vocabulary is shared across the workstream and is
// defined by the telemetry record schema, which is the superset and the
// validated one; Atlas adopts it rather than keeping its own names (ADR-0010).
// Renaming these is a breaking change to atlas.json: a consumer joining "what
// exists" to "what is used" matches on this value.
//
// Atlas emits only the kinds it can observe by harvesting a tree. The shared
// vocabulary also defines mcp_tool, plugin and builtin_tool, which are
// invocation-time observations rather than things on disk; their absence here
// is a limit of harvesting, not a disagreement about naming.
const (
	TypeSkill     = "skill"
	TypeSubagent  = "subagent"
	TypeHook      = "hook"
	TypeCommand   = "command"
	TypeMCPServer = "mcp_server"
)

// Primitive is one governable unit inside a package.
type Primitive struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Install holds the commands rendered for a package. Both are derived from
// manifest fields; a missing command is correct, a guessed one is a defect.
// Both fields carry omitempty: unlike Primitives, a zero value here (a
// command that was not constructed — no sourceBase, or a repo source with no
// install path) carries no meaning, so it must be omitted rather than
// emitted as "".
type Install struct {
	MarketplaceAdd string `json:"marketplaceAdd,omitempty"`
	Install        string `json:"install,omitempty"`
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

// Warning records a signal that does not block generation but should be
// visible — e.g. a descriptor exclude pattern that matched nothing. Like
// Collisions, the field carries no omitempty on Atlas.Warnings, so a nil
// slice and an empty slice marshal differently. That distinction only
// holds if a producer upholds it: a producer populating Atlas.Warnings must
// initialise it to a non-nil empty slice ([]Warning{}), not leave it nil,
// once it has checked for warnings at all. [] marshals to "warnings": []
// ("checked, none found"); nil marshals to "warnings": null ("not populated
// by this producer") — and those are different claims to a consumer. The
// struct tag alone cannot guarantee this; it is an invariant the producer
// must uphold.
type Warning struct {
	Kind   string `json:"kind"` // e.g. "unused-exclude"
	Source string `json:"source"`
	Detail string `json:"detail"`
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
	Warnings      []Warning   `json:"warnings"`
}

// MarshalJSONIndent renders atlas.json in its on-disk form.
func (a *Atlas) MarshalJSONIndent() ([]byte, error) {
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
