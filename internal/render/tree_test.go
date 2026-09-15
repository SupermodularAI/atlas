package render

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/SupermodularAI/atlas/internal/model"
)

// atlasWithDegradation is the fixture the §7 tests share: one readable source
// holding a harvested, a restricted and an excluded package, plus a source
// Atlas could not reach at all. Every degradation level appears exactly once,
// so a test that loses one loses it visibly.
func atlasWithDegradation() *model.Atlas {
	return &model.Atlas{
		SchemaVersion: model.SchemaVersion,
		Company:       "example-co",
		Sources: []model.Source{
			{Name: "mkt", Kind: "marketplace", Status: model.StatusRead},
			{Name: "gone", Kind: "marketplace", Status: model.StatusUnavailable, Reason: "clone failed"},
		},
		Packages: []model.Package{
			{
				Name: "pkg-open", Source: "mkt", Access: model.AccessPublic,
				Primitives: []model.Primitive{
					{Type: model.TypeSkill, Name: "alpha", Description: "first"},
					{Type: model.TypeHook, Name: "guard.sh"},
				},
			},
			{
				Name: "pkg-locked", Source: "mkt", Access: model.AccessRestricted,
				Reason: "access denied", Primitives: nil,
			},
			{
				Name: "pkg-held", Source: "mkt", Access: model.AccessExcluded,
				Reason: "excluded by descriptor", Primitives: nil,
			},
			{
				Name: "pkg-empty", Source: "mkt", Access: model.AccessPublic,
				Primitives: []model.Primitive{},
			},
		},
	}
}

// A tree layout draws only the nodes it is handed. A builder that skipped
// packages with no primitives would silently omit exactly the restricted and
// excluded ones — the silent truncation §7 exists to prevent, and the failure
// this whole feature is most likely to reintroduce.
func TestBuildTreeKeepsDegradedPackagesVisible(t *testing.T) {
	tree := buildTree(atlasWithDegradation())

	found := map[string]string{} // package name -> state
	var walk func(treeNode)
	walk = func(n treeNode) {
		if n.Kind == "package" {
			found[n.Name] = n.State
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(tree)

	for _, want := range []struct{ name, state string }{
		{"pkg-open", model.AccessPublic},
		{"pkg-locked", model.AccessRestricted},
		{"pkg-held", model.AccessExcluded},
		{"pkg-empty", model.AccessPublic},
	} {
		got, ok := found[want.name]
		if !ok {
			t.Errorf("package %q is absent from the tree: a package Atlas reported must never vanish from the map (design §7)", want.name)
			continue
		}
		if got != want.state {
			t.Errorf("package %q has state %q, want %q", want.name, got, want.state)
		}
	}
}

// §7 requires the two degradation levels stay distinguishable. Collapsing them
// into one "missing" state would make the map claim less than atlas.json does.
func TestBuildTreeKeepsDegradationLevelsDistinct(t *testing.T) {
	tree := buildTree(atlasWithDegradation())

	var states []string
	var walk func(treeNode)
	walk = func(n treeNode) {
		if n.Kind == "package" {
			states = append(states, n.State)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(tree)

	var restricted, excluded int
	for _, s := range states {
		switch s {
		case model.AccessRestricted:
			restricted++
		case model.AccessExcluded:
			excluded++
		}
	}
	if restricted != 1 || excluded != 1 {
		t.Fatalf("got %d restricted and %d excluded package nodes, want exactly 1 of each — the two levels must not be collapsed (design §7)", restricted, excluded)
	}
}

// An unavailable source carries its status onto its node. A source that
// rendered identically to a read one would hide that coverage was bounded.
func TestBuildTreeMarksUnavailableSource(t *testing.T) {
	tree := buildTree(atlasWithDegradation())

	var gone *treeNode
	for i := range tree.Children {
		if tree.Children[i].Name == "gone" {
			gone = &tree.Children[i]
		}
	}
	if gone == nil {
		t.Fatal("unavailable source is absent from the tree; it must be visible, not silently dropped")
	}
	if gone.State != model.StatusUnavailable {
		t.Errorf("unavailable source has state %q, want %q", gone.State, model.StatusUnavailable)
	}
	if gone.Reason == "" {
		t.Error("unavailable source carries no reason; an operator must be able to read why without leaving the map")
	}
}

// Node size encodes harvested primitive count and nothing else. Sizing by
// usage, popularity or endorsement would widen the claim (design §9), so the
// count must equal exactly what was harvested.
func TestBuildTreeCountsOnlyHarvestedPrimitives(t *testing.T) {
	tree := buildTree(atlasWithDegradation())

	counts := map[string]int{}
	var walk func(treeNode)
	walk = func(n treeNode) {
		if n.Kind == "package" {
			counts[n.Name] = n.Count
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(tree)

	if counts["pkg-open"] != 2 {
		t.Errorf("pkg-open count = %d, want 2", counts["pkg-open"])
	}
	// Both a restricted package and a genuinely empty one count zero. They are
	// told apart by State, never by size — which is why size alone must not be
	// the only signal in the rendering.
	if counts["pkg-locked"] != 0 {
		t.Errorf("pkg-locked count = %d, want 0: nothing was harvested", counts["pkg-locked"])
	}
	if counts["pkg-empty"] != 0 {
		t.Errorf("pkg-empty count = %d, want 0", counts["pkg-empty"])
	}
}

// Every package node must address a real card, so clicking the map lands on
// the detail the page already renders rather than scrolling nowhere.
func TestBuildTreePackageNodesCarryCardID(t *testing.T) {
	a := atlasWithDegradation()
	tree := buildTree(a)

	var walk func(treeNode)
	walk = func(n treeNode) {
		if n.Kind == "package" && n.CardID == "" {
			t.Errorf("package %q has no cardId; the node would be unclickable", n.Name)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(tree)

	// The id must be byte-identical to the one the card renders with, or the
	// link silently scrolls nowhere. CardID is the single source of truth for
	// both; assert the agreement rather than trusting it.
	want := CardID("mkt", "pkg-open")
	var got string
	walk2 := func(n treeNode) {
		for _, s := range n.Children {
			for _, p := range s.Children {
				if p.Name == "pkg-open" {
					got = p.CardID
				}
			}
		}
	}
	walk2(tree)
	if got != want {
		t.Errorf("cardId = %q, want %q (must match the rendered card exactly)", got, want)
	}
}

// The tree JSON lands inside a <script> block. A harvested description
// containing "</script>" must not be able to close it early.
//
// json.Marshal escapes <, > and & by default; this test pins that default,
// so switching to an Encoder with SetEscapeHTML(false) fails loudly here
// instead of silently reopening the hole.
func TestTreeJSONEscapesScriptClose(t *testing.T) {
	a := &model.Atlas{
		SchemaVersion: model.SchemaVersion,
		Company:       "example-co",
		Sources:       []model.Source{{Name: "mkt", Kind: "marketplace", Status: model.StatusRead}},
		Packages: []model.Package{{
			Name: "evil", Source: "mkt", Access: model.AccessPublic,
			Primitives: []model.Primitive{{
				Type:        model.TypeSkill,
				Name:        `</script><script>alert(1)</script>`,
				Description: `also </SCRIPT> and <img src=x onerror=alert(2)>`,
			}},
		}},
	}

	got, err := treeJSON(a)
	if err != nil {
		t.Fatalf("treeJSON: %v", err)
	}

	// The literal characters that could terminate the element must not survive.
	for _, bad := range []string{"<", ">", "</script", "</SCRIPT"} {
		if strings.Contains(got, bad) {
			t.Errorf("tree JSON contains raw %q — a harvested string could close the script element", bad)
		}
	}
	// The payload must still round-trip: escaping that lost data would be a
	// different defect.
	var back treeNode
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("tree JSON does not parse: %v", err)
	}
	name := back.Children[0].Children[0].Children[0].Name
	if name != `</script><script>alert(1)</script>` {
		t.Errorf("name did not round-trip: got %q", name)
	}
}

// The page embeds the tree as a JSON value, not as a JS string literal. When
// this regressed during development the block parsed to a string rather than
// an object and the view silently did not draw — no error, no empty state,
// just a missing map. Assert the shape that failure would break.
func TestRenderTreeDataIsParseableJSON(t *testing.T) {
	html, err := Render(atlasWithDegradation())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	const open = `<script id="radial-data" type="application/json">`
	i := strings.Index(string(html), open)
	if i < 0 {
		t.Fatal("radial data block is absent from the page")
	}
	rest := string(html)[i+len(open):]
	j := strings.Index(rest, "</script>")
	if j < 0 {
		t.Fatal("radial data block is not closed")
	}
	payload := rest[:j]

	if strings.HasPrefix(strings.TrimSpace(payload), `"`) {
		t.Fatal("tree data is a quoted JS string literal, not a JSON object: JSON.parse would yield a string and the view would not draw")
	}
	var node treeNode
	if err := json.Unmarshal([]byte(payload), &node); err != nil {
		t.Fatalf("embedded tree data does not parse as JSON: %v", err)
	}
	if node.Kind != "root" || len(node.Children) != 2 {
		t.Errorf("parsed tree = kind %q with %d children, want root with 2", node.Kind, len(node.Children))
	}
}

// ADR-0001 pins D3 by checksum rather than by URL: a CDN URL is a mutable
// pointer, a checksum is not. This is the same reasoning Atlas gives its own
// consumers for pinning release binaries by SHA256 rather than by tag.
func TestVendoredD3IsPinned(t *testing.T) {
	const (
		wantSHA  = "f2094bbf6141b359722c4fe454eb6c4b0f0e42cc10cc7af921fc158fceb86539"
		wantSize = 279706
	)
	if len(d3JS) != wantSize {
		t.Errorf("vendored D3 is %d bytes, want %d — the pinned artifact changed", len(d3JS), wantSize)
	}
	sum := sha256.Sum256([]byte(d3JS))
	if got := hex.EncodeToString(sum[:]); got != wantSHA {
		t.Errorf("vendored D3 sha256 = %s, want %s (ADR-0001 pins this; update the ADR deliberately, never the test alone)", got, wantSHA)
	}
}

// D3 is ISC-licensed: the copyright notice must appear in all copies. Every
// generated page is a copy, so stripping the banner would make each rendered
// atlas a licence violation. That makes this a correctness constraint on the
// template, not a matter of tidiness.
func TestVendoredD3KeepsLicenceBanner(t *testing.T) {
	const notice = d3BannerNotice
	if !strings.Contains(d3JS, notice) {
		t.Fatalf("vendored D3 has lost its copyright banner (%q)", notice)
	}

	html, err := Render(atlasWithDegradation())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(html), notice) {
		t.Error("rendered page does not carry D3's copyright notice; ISC requires it in all copies, and every generated atlas is a copy")
	}
}

// §10: the page makes no external requests. Vendoring D3 was the whole point
// of ADR-0001, so a regression to a CDN <script src> must fail loudly.
func TestRenderedPageMakesNoExternalRequests(t *testing.T) {
	html, err := Render(atlasWithDegradation())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, bad := range []string{"src=\"http", "src='http", "href=\"http://", "d3js.org/d3", "cdn.jsdelivr", "unpkg.com"} {
		if strings.Contains(string(html), bad) {
			t.Errorf("page references %q: an atlas must render from disk with no network (design §10)", bad)
		}
	}
}

// TestRenderHostilePageForBrowser writes a page whose harvested strings are
// attack payloads, for loading in a real browser.
//
// The string assertions above prove the bytes are escaped; only a browser
// proves the escaping holds once a parser and D3 have both had the document.
// Skipped by default — it writes a file and is a manual aid, not a gate.
// Run with: go test ./internal/render -run HostilePageForBrowser -xss-out=/tmp/x.html
func TestRenderHostilePageForBrowser(t *testing.T) {
	if *xssOut == "" {
		t.Skip("set -xss-out=<path> to write the hostile page")
	}
	a := &model.Atlas{
		SchemaVersion: model.SchemaVersion,
		Company:       "example-co",
		Sources:       []model.Source{{Name: "mkt", Kind: "marketplace", Status: model.StatusRead}},
		Packages: []model.Package{{
			Name:        "pkg-evil",
			Source:      "mkt",
			Access:      model.AccessPublic,
			Description: `</script><script>window.XSS_DESC=1</script>`,
			Primitives: []model.Primitive{
				{Type: model.TypeSkill, Name: `</script><script>window.XSS_NAME=1</script>`, Description: "payload in name"},
				{Type: model.TypeSkill, Name: "img-payload", Description: `<img src=x onerror="window.XSS_IMG=1">`},
			},
		}},
		Summary: model.Summary{
			Sources:  map[string]int{"read": 1, "unavailable": 0},
			Packages: map[string]int{"harvested": 1, "restricted": 0, "excluded": 0},
		},
	}
	html, err := Render(a)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if err := os.WriteFile(*xssOut, html, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Logf("wrote %s (%d bytes)", *xssOut, len(html))
}

var xssOut = flag.String("xss-out", "", "path to write the hostile-payload page (manual browser check)")

// TestRenderRealScalePageForBrowser writes a page at the size Atlas actually
// meets in production, for loading in a real browser.
//
// The shape comes from docs/first-run.md: 8 packages, 73 primitives across 6 of
// them, 2 withheld. This matters because this page has already been wrong at
// scale once — the flat list in page.gohtml was "correct for the 4-primitive
// fixture it was reviewed against and did not survive the scale". The committed
// example fixture is 2 packages and 2 primitives, so it cannot catch a repeat.
//
// Skipped by default; it writes a file and is a manual aid, not a gate.
// Run with: go test ./internal/render -run RealScalePageForBrowser -scale-out=/tmp/s.html
func TestRenderRealScalePageForBrowser(t *testing.T) {
	if *scaleOut == "" {
		t.Skip("set -scale-out=<path> to write the real-scale page")
	}
	types := []string{model.TypeSkill, model.TypeSubagent, model.TypeHook, model.TypeCommand, model.TypeMCPServer}
	a := &model.Atlas{
		SchemaVersion: model.SchemaVersion,
		Company:       "example-co",
		Sources:       []model.Source{{Name: "example-mkt", Kind: "marketplace", Status: model.StatusRead}},
	}
	// 73 primitives spread over 6 packages, as the real run recorded.
	spread := []int{18, 15, 13, 11, 9, 7}
	n := 0
	for i, count := range spread {
		pkg := model.Package{
			Name:        fmt.Sprintf("pkg-service-%d", i+1),
			Source:      "example-mkt",
			Access:      model.AccessPublic,
			Description: "A package harvested from the marketplace.",
		}
		for j := 0; j < count; j++ {
			pkg.Primitives = append(pkg.Primitives, model.Primitive{
				Type:        types[n%len(types)],
				Name:        fmt.Sprintf("primitive-name-%02d", n),
				Description: "A harvested primitive with a description of realistic length.",
			})
			n++
		}
		a.Packages = append(a.Packages, pkg)
	}
	a.Packages = append(a.Packages,
		model.Package{Name: "pkg-confidential", Source: "example-mkt", Access: model.AccessExcluded,
			Reason: "excluded by descriptor", Description: "Withheld."},
		model.Package{Name: "pkg-locked", Source: "example-mkt", Access: model.AccessRestricted,
			Reason: "access denied: could not read Username", Description: "Unreadable."},
	)
	a.Summary = model.Summary{
		Sources:  map[string]int{"read": 1, "unavailable": 0},
		Packages: map[string]int{"harvested": 6, "restricted": 1, "excluded": 1},
	}

	html, err := Render(a)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if err := os.WriteFile(*scaleOut, html, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Logf("wrote %s (%d bytes, %d primitives across %d packages)", *scaleOut, len(html), n, len(a.Packages))
}

var scaleOut = flag.String("scale-out", "", "path to write the real-scale page (manual browser check)")

// d3BannerNotice is the upstream copyright line ISC requires to survive into
// every copy. It doubles as the marker that locates the vendored block.
const d3BannerNotice = "Copyright 2010-2023 Mike Bostock"

// atlasOwnScripts returns the rendered page with the vendored D3 block removed,
// so a test asserting a property of code Atlas wrote is not answered by
// third-party bytes.
//
// It fails rather than returning the page unchanged when the block is not
// found: silently widening the scan back to the whole document is how this
// helper would stop meaning anything, and a test that quietly checks more than
// it claims is worse than one that breaks.
func atlasOwnScripts(t *testing.T, page string) string {
	t.Helper()

	i := strings.Index(page, d3BannerNotice)
	if i < 0 {
		t.Fatalf("vendored D3 block not found in the page (looked for %q); "+
			"if D3 was removed, delete this helper rather than letting it scan everything", d3BannerNotice)
	}
	start := strings.LastIndex(page[:i], "<script>")
	if start < 0 {
		t.Fatal("vendored D3 is not inside a script element")
	}
	end := strings.Index(page[start:], "</script>")
	if end < 0 {
		t.Fatal("vendored D3 script element is unterminated")
	}
	return page[:start] + page[start+end+len("</script>"):]
}
