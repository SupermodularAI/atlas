// Package render turns an atlas into a self-contained HTML page.
//
// html/template escapes every interpolation by default, which is the injection
// guard for third-party frontmatter text — internal/harvest deliberately
// returns descriptions verbatim, including markup, so this is the only place
// that escaping happens. Never switch to text/template or build HTML by
// string concatenation: either would silently reopen the hole this package
// exists to close.
package render

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"

	"github.com/SupermodularAI/atlas/internal/model"
)

//go:embed page.gohtml
var files embed.FS

// d3JS is D3 v7.9.0, vendored and inlined into every generated page (ADR-0001).
//
// It is embedded rather than fetched so §10's self-containment holds: an atlas
// still opens from disk, from a commit, or from a host with no egress. The
// bytes are pinned by checksum in TestVendoredD3IsPinned — a CDN URL is a
// mutable pointer, a checksum is not.
//
// D3 is ISC-licensed and its copyright banner must appear in all copies. Every
// generated page is a copy, so the banner has to survive into the output;
// TestVendoredD3KeepsLicenceBanner asserts it does.
//
//go:embed vendor/d3.v7.min.js
var d3JS string

// Render produces the complete page for a. No external requests: the CSS and
// the one script are inline and nothing is fetched, so a generated atlas opens
// correctly straight from disk, including over file://.
func Render(a *model.Atlas) ([]byte, error) {
	// cardID is the single source of truth for a card's DOM id and for the
	// fragment that addresses it. Computing it in one place is what keeps the
	// two byte-identical; see CardID for why that matters.
	t, err := template.New("page.gohtml").Funcs(template.FuncMap{
		"cardID": CardID,
	}).ParseFS(files, "page.gohtml")
	if err != nil {
		return nil, fmt.Errorf("render: parse template: %w", err)
	}

	tree, err := treeJSON(a)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, pageData{Atlas: a, TreeJSON: template.JS(tree), D3: template.JS(d3JS)}); err != nil {
		return nil, fmt.Errorf("render: execute template: %w", err)
	}
	return buf.Bytes(), nil
}

// pageData is what the template executes against.
//
// Atlas is embedded rather than held in a named field so every existing
// {{ .Packages }} / {{ .Sources }} reference in page.gohtml keeps working
// unchanged — the radial view is additive, and a template-wide rename would
// have been a much larger diff for no behavioural gain.
type pageData struct {
	*model.Atlas
	// TreeJSON is the radial hierarchy, already JSON-encoded.
	//
	// It is template.JS, not string: inside <script type="application/json">
	// html/template treats a plain string as a JS string *literal* and emits
	// it quoted and backslash-escaped, so JSON.parse would yield a string
	// rather than an object and the view would silently not draw. The bytes
	// must land as the JSON value they already are.
	//
	// Bypassing the template's escaping here is safe because treeJSON's
	// json.Marshal has already escaped <, > and & to <, > and
	// & — so no harvested description can close the script element.
	// TestTreeJSONEscapesScriptClose pins that property, and
	// TestRenderTreeDataIsParseableJSON pins this one.
	TreeJSON template.JS
	// D3 is the vendored library. It is template.JS because it is our own
	// pinned, checksum-asserted artifact rather than harvested input — the
	// one place in this package where escaping is deliberately bypassed, and
	// only because escaping a JS library would corrupt it.
	D3 template.JS
}
