package render

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SupermodularAI/atlas/internal/build"
	"github.com/SupermodularAI/atlas/internal/descriptor"
	"github.com/SupermodularAI/atlas/internal/model"
)

func sample() *model.Atlas {
	return &model.Atlas{
		SchemaVersion: model.SchemaVersion,
		Company:       "acme",
		GeneratedAt:   "2026-08-18T00:00:00Z",
		Sources: []model.Source{
			{Name: "mkt", Kind: "marketplace", Status: model.StatusRead, Owner: "acme", Version: "1.0.0"},
			{Name: "gone", Kind: "marketplace", Status: model.StatusUnavailable, Reason: "fetch failed: 404"},
		},
		Packages: []model.Package{
			{Name: "pkg-open", Source: "mkt", Description: "Open pkg.", Access: model.AccessPublic,
				ResolvedSha: "abc1234def5678", Primitives: []model.Primitive{
					{Type: model.TypeSkill, Name: "code-review", Description: "Reviews code."},
				},
				Install: &model.Install{MarketplaceAdd: "apm marketplace add u --name mkt", Install: "apm install pkg-open@mkt --target claude"}},
			{Name: "pkg-secret", Source: "mkt", Description: "Confidential.", Access: model.AccessExcluded,
				Reason: "excluded by descriptor", Primitives: nil},
			{Name: "pkg-locked", Source: "mkt", Description: "Unreadable.", Access: model.AccessRestricted,
				Reason: "clone failed: 403", Primitives: nil},
		},
		Collisions: []model.Collision{
			{Kind: "package-name", Name: "dup", Sources: []string{"mkt", "other"}},
		},
		Summary: model.Summary{
			Sources:  map[string]int{"read": 1, "unavailable": 1},
			Packages: map[string]int{"harvested": 1, "restricted": 1, "excluded": 1},
		},
		Warnings: []model.Warning{
			{Kind: "unused-exclude", Source: "mkt", Detail: `exclude pattern "typo-pkg" matched nothing`},
			{Kind: "duplicate-primitive", Source: "mkt", Detail: `duplicate skill "dup-prim": kept a, dropped b`},
		},
	}
}

func TestRenderIncludesCoreContent(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		"acme", "pkg-open", "code-review", "Reviews code.",
		"pkg-secret", "pkg-locked", "dup",
		"apm install pkg-open@mkt --target claude",
		"2026-08-18T00:00:00Z",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rendered page missing %q", want)
		}
	}
}

func TestRenderIsSelfContained(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	// The property (§10): the page's own markup and styling make no external
	// request. Checked as "no external scheme at all", not as an enumerated
	// list of known-bad forms — a list only catches the forms it names (an
	// SVG xlink:href, an @font-face src, image-set(...), etc. would all slip
	// past a literal list unnoticed).
	//
	// Scoped to <head>...</head> — the region that can affect what the page
	// fetches (stylesheets, scripts, fonts, the inline <style> block) — rather
	// than the whole document. Harvested body content (a package description,
	// an `apm marketplace add <url> ...` install command) can legitimately
	// contain an http(s) URL as inert escaped text; a page-wide ban would
	// flag that as a false failure. Nothing legitimate belongs in <head>.
	headStart := strings.Index(s, "<head>")
	headEnd := strings.Index(s, "</head>")
	if headStart == -1 || headEnd == -1 || headEnd < headStart {
		t.Fatalf("rendered page missing a well-formed <head>...</head> region")
	}
	head := s[headStart:headEnd]
	if strings.Contains(head, "http://") || strings.Contains(head, "https://") {
		t.Errorf("page <head> must reference no external scheme, found one in:\n%s", head)
	}

	// Keep the literal checks too: they add specificity to the failure
	// message when a known-bad form appears, even though the scheme check
	// above is the actual guard.
	for _, forbidden := range []string{
		"<script src", `<link rel="stylesheet" href`, "@import", "<img src=\"http",
		"fonts.googleapis", "fonts.gstatic", "cdn.",
	} {
		if strings.Contains(s, forbidden) {
			t.Errorf("page must make no external requests, found %q", forbidden)
		}
	}
}

// Guarantee test: harvested markup must be inert.
func TestRenderEscapesHarvestedMarkup(t *testing.T) {
	a := sample()
	a.Packages[0].Primitives[0].Description = `<script>alert(1)</script>`
	out, err := Render(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "<script>alert(1)</script>") {
		t.Error("harvested markup was rendered live — it must be escaped")
	}
	if !strings.Contains(s, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("expected the markup escaped into entities, adjacent as one unit")
	}
}

// The jump index put the package name into an href and an id attribute, which
// is a different escaping context than body text. Package names come from a
// marketplace manifest, so they are external input. The constant "#pkg-"/"pkg-"
// prefix is what makes this safe: a name can never begin the URL, so it cannot
// introduce a javascript: scheme.
func TestRenderEscapesPackageNameInAttributes(t *testing.T) {
	a := sample()
	a.Packages[0].Name = `x" onmouseover="alert(1)`
	out, err := Render(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, `onmouseover="alert(1)"`) {
		t.Error("package name broke out of the attribute — an event handler was injected")
	}
	if strings.Contains(s, `"alert(1)`) && !strings.Contains(s, "&#34;") {
		t.Error("expected the quote in the package name escaped inside the attribute")
	}
	// The href must still be a same-page fragment, never a new scheme.
	if strings.Contains(strings.ToLower(s), "href=\"javascript:") {
		t.Error("a javascript: URL reached an href")
	}
}

func TestRenderPackageNameCannotStartHref(t *testing.T) {
	a := sample()
	a.Packages[0].Name = "javascript:alert(1)"
	out, err := Render(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// The constant prefix must survive, keeping the value a fragment.
	if !strings.Contains(s, "#pkg-javascript:alert(1)") && !strings.Contains(s, "#pkg-javascript") {
		t.Error("expected the name confined behind the #pkg- prefix")
	}
	if strings.Contains(strings.ToLower(s), `href="javascript:`) {
		t.Error("the name escaped the prefix and became the URL scheme")
	}
}

// The filter hides index rows by setting the hidden attribute, which takes
// effect only through the UA rule [hidden] { display: none }. Because the
// stylesheet gives ul.index li an author `display`, that author declaration
// wins the cascade and hidden silently stops working — the row keeps rendering
// while li.hidden reads true, so the index contradicts the search count.
// Caught only by measuring computed style in a browser; asserting on
// element.hidden passes either way, because it is the input to the behaviour
// rather than the behaviour. This test pins the override so the pair cannot
// drift apart again.
func TestIndexRowsCanActuallyHide(t *testing.T) {
	a := sample()
	out, err := Render(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "ul.index li[hidden]") {
		t.Fatal("no [hidden] override for index rows: the author display rule beats " +
			"the UA [hidden] rule, so filtering the index would not hide anything")
	}
	// The override is only meaningful if it actually removes the row from layout.
	idx := strings.Index(s, "ul.index li[hidden]")
	rule := s[idx:]
	if end := strings.Index(rule, "}"); end != -1 {
		rule = rule[:end]
	}
	if !strings.Contains(strings.ReplaceAll(rule, " ", ""), "display:none") {
		t.Errorf("the [hidden] override must set display:none, got: %q", rule)
	}
}

func TestRenderDistinguishesExcludedFromRestricted(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "access restricted") {
		t.Error("a restricted package must say so on the page")
	}
	if !strings.Contains(s, "withheld by descriptor") {
		t.Error("an excluded package must be distinguishable from a restricted one")
	}
	// Distinctness, not mere presence: the two states must not share one marker.
	if strings.Count(s, "access restricted") == 0 || strings.Count(s, "withheld by descriptor") == 0 {
		t.Fatal("both markers must independently appear")
	}
}

// Pins the exact class="..." string per Access value. Package.Access is the
// only harvested value ever interpolated into an attribute context (page.gohtml's
// card div); this test protects that surface across a refactor to a literal
// eq/else-if branch (no interpolated value in attribute context at all), by
// asserting the produced attribute text stays byte-identical.
func TestRenderCardClassPerAccessValue(t *testing.T) {
	a := sample()
	out, err := Render(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		`class="card"`,                     // pkg-open: public
		`class="card withheld excluded"`,   // pkg-secret: excluded
		`class="card withheld restricted"`, // pkg-locked: restricted
	} {
		if !strings.Contains(s, want) {
			t.Errorf("expected card class attribute %q, not found in rendered page", want)
		}
	}
}

func TestRenderRendersUnavailableSourceAsStub(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "could not be") {
		t.Error("an unavailable source must render as a distinct stub, not a locked package")
	}
	if !strings.Contains(s, "fetch failed: 404") {
		t.Error("the unavailable source's reason must be shown")
	}
}

func TestRenderStatesTheClaimBoundary(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "not assert") {
		t.Error("the page must state what Atlas does not assert (spec §9)")
	}
	// Positive half: the disclaimer prose about published/SHA/read-at must
	// actually be present, not merely absent of banned words (which an empty
	// page would also satisfy).
	for _, want := range []string{"published", "resolved", "read at"} {
		if !strings.Contains(s, want) {
			t.Errorf("claim-boundary disclaimer missing %q", want)
		}
	}
}

func TestRenderNeverClaimsApproval(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(out))
	// "approved" may appear only inside the explicit negated disclaimer
	// sentence ("does not assert ... approved"); it must never appear as an
	// affirmative claim about a specific package or primitive.
	for _, banned := range []string{"approved primitive", "verified unaltered", "tamper-evident", "is approved", "was approved"} {
		if strings.Contains(lower, banned) {
			t.Errorf("page must not claim %q", banned)
		}
	}
}

func TestRenderRendersWarnings(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// The Detail text goes through html/template like any other harvested
	// string, so its quotes come out HTML-escaped (&#34;) — asserting on the
	// literal quote here would be asserting on the wrong (unescaped) output.
	if !strings.Contains(s, "unused-exclude") || !strings.Contains(s, "typo-pkg") || !strings.Contains(s, "matched nothing") {
		t.Error("an unused-exclude warning must render")
	}
	if !strings.Contains(s, "duplicate-primitive") || !strings.Contains(s, "dup-prim") || !strings.Contains(s, "kept a, dropped b") {
		t.Error("a duplicate-primitive warning must render")
	}
}

func TestRenderOmitsInstallWhenAbsent(t *testing.T) {
	a := sample()
	a.Packages[0].Install = nil
	out, err := Render(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "apm install") || strings.Contains(s, "apm marketplace add") {
		t.Error("no install command must be rendered when Package.Install is nil")
	}
}

// --- Empty state and shareable ?q= (defects 6 and 7) -----------------------
//
// These guard a static <script> block that Go cannot execute, so each test
// pins the *shape* the browser-verified behaviour depends on, at the place
// where a regression would actually be introduced. The runtime behaviour
// itself (computed display, painted height, an inert injection payload) was
// measured in a headless browser against this template; these tests keep the
// preconditions for that behaviour from silently drifting.

// styleBlock returns the contents of the page's <style> element.
func styleBlock(t *testing.T, page string) string {
	t.Helper()
	start := strings.Index(page, "<style>")
	end := strings.Index(page, "</style>")
	if start == -1 || end == -1 || end < start {
		t.Fatal("rendered page has no well-formed <style>...</style> block")
	}
	return page[start+len("<style>") : end]
}

// templateScriptBlock returns the <script> block as it is written in the
// template *source*, read through the same embed.FS the renderer parses.
//
// Reading the rendered page would be wrong for any assertion about template
// actions: by then html/template has already executed them, so a smuggled
// {{ .Company }} inside the script has become its value and the telltale
// braces are gone. Source is the only place that invariant is observable.
func templateScriptBlock(t *testing.T) string {
	t.Helper()
	raw, err := files.ReadFile("page.gohtml")
	if err != nil {
		t.Fatalf("read embedded template: %v", err)
	}
	src := string(raw)
	start := strings.Index(src, "<script>")
	end := strings.Index(src, "</script>")
	if start == -1 || end == -1 || end < start {
		t.Fatal("template has no well-formed <script>...</script> block")
	}
	return src[start+len("<script>") : end]
}

// scriptBlock returns the contents of the page's <script> element.
func scriptBlock(t *testing.T, page string) string {
	t.Helper()
	start := strings.Index(page, "<script>")
	end := strings.Index(page, "</script>")
	if start == -1 || end == -1 || end < start {
		t.Fatal("rendered page has no well-formed <script>...</script> block")
	}
	return page[start+len("<script>") : end]
}

// jsFunctionBody returns the body of the named function declaration in the
// page's script, from its opening brace to the matching close.
//
// Needed because an unscoped strings.Contains over the whole script block
// cannot assert anything about a specific construct inside it. Two assertions
// in this file were proved worthless exactly that way: a check for "catch" was
// satisfied by an unrelated catch elsewhere in the script, and a check for
// "searchParams" was satisfied by the read-back call, so deleting syncURL's
// own guard or swapping its encoding for string concatenation left both tests
// green. Scoping the search to one function body is what gives them teeth.
func jsFunctionBody(t *testing.T, js, name string) string {
	t.Helper()
	decl := "function " + name + "("
	i := strings.Index(js, decl)
	if i == -1 {
		t.Fatalf("no %s declaration in the page script", decl)
	}
	open := strings.Index(js[i:], "{")
	if open == -1 {
		t.Fatalf("no opening brace for %s", name)
	}
	open += i
	// Brace matching rather than a search for the next "}": the body contains
	// nested blocks, so the first close brace is not the function's.
	depth := 0
	for j := open; j < len(js); j++ {
		switch js[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return js[open+1 : j]
			}
		}
	}
	t.Fatalf("unbalanced braces in %s", name)
	return ""
}

// The empty state must exist in the served HTML and start hidden. Creating it
// from JS instead would leave a JS-off reader with no element at all, and
// shipping it without `hidden` would paint the no-match message above the
// full listing on every unfiltered load.
func TestRenderEmptyStateIsInMarkupAndStartsHidden(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	i := strings.Index(s, `id="empty"`)
	if i == -1 {
		t.Fatal(`no element with id="empty": the empty state must be rendered by the ` +
			`template, not created in JS, so the page degrades with JS off`)
	}
	// Inspect the element's own tag, so `hidden` cannot be satisfied by an
	// attribute that happens to sit on some other element nearby.
	tagStart := strings.LastIndex(s[:i], "<")
	tagEnd := strings.Index(s[i:], ">")
	if tagStart == -1 || tagEnd == -1 {
		t.Fatal("could not isolate the empty-state element's tag")
	}
	tag := s[tagStart : i+tagEnd]
	if !strings.Contains(tag, "hidden") {
		t.Errorf("the empty state must carry the hidden attribute in the served HTML, got: %q", tag)
	}
	if !strings.Contains(s, `id="emptyterm"`) {
		t.Error(`no id="emptyterm" span: the term needs its own text-only node to be set into`)
	}
	if !strings.Contains(s, "No packages or primitives match") {
		t.Error("the empty state must explain itself in the content area, " +
			"not leave the count beside the input as the only feedback")
	}
	if !strings.Contains(s, `id="clearq"`) {
		t.Error("the empty state must offer a control to clear the filter")
	}
}

// Source headings are hidden as a unit, so the h2 and its owner/version line
// must live inside one element the filter can toggle. Without the wrapper the
// JS would have to walk siblings, and a heading would strand itself above the
// empty state.
func TestRenderWrapsEachSourceInAToggleableSection(t *testing.T) {
	a := sample()
	out, err := Render(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if got, want := strings.Count(s, `<section class="src">`), len(a.Sources); got != want {
		t.Errorf("got %d source sections, want one per source (%d)", got, want)
	}
	if got, want := strings.Count(s, "</section>"), len(a.Sources); got != want {
		t.Errorf("got %d closing </section> tags, want %d — sections must not nest or leak", got, want)
	}
	// The owner/version meta line must sit inside the section, otherwise
	// hiding the section leaves it stranded — the exact defect being fixed.
	secStart := strings.Index(s, `<section class="src">`)
	secEnd := strings.Index(s, "</section>")
	if secStart == -1 || secEnd == -1 || secEnd < secStart {
		t.Fatal("no well-formed source section")
	}
	first := s[secStart:secEnd]
	if !strings.Contains(first, "Owner acme") {
		t.Error("the owner/version meta line must be inside the source section, " +
			"so it is hidden together with the heading it belongs to")
	}
}

// The cascade trap, stated as a property rather than as a string match.
//
// The hidden attribute takes effect only through the UA rule
// [hidden] { display: none }, which ANY author rule setting display on the
// same element defeats. So for every selector the filter toggles hidden on,
// the stylesheet must either declare no display for it or carry a matching
// [hidden] { display: none } override. Asserting merely that a particular
// override string is present would be theatre: with no author display rule
// for that selector, deleting the override breaks nothing.
func TestToggledElementsHaveNoUnguardedDisplayRule(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	css := styleBlock(t, string(out))

	// Every selector the script sets .hidden on. Keep in step with the script:
	// a toggled selector missing from this list is unguarded silently.
	//
	// Covers top-level rules only — the splitter cannot see inside an @media
	// block. Adequate because a [hidden] override outspecifies its bare
	// selector wherever it is declared, but a display added only inside a
	// media query for a new selector would need checking by hand.
	for _, sel := range []string{"ul.index li", "ul.prims li", ".empty", "section.src", ".card"} {
		declaresDisplay := false
		for _, rule := range strings.Split(css, "}") {
			head, body, ok := strings.Cut(rule, "{")
			if !ok {
				continue
			}
			// Exact selector match only: ".card" must not be credited with
			// (or blamed for) a rule written for ".card h3".
			matches := false
			for _, part := range strings.Split(head, ",") {
				if strings.TrimSpace(part) == sel {
					matches = true
				}
			}
			if matches && strings.Contains(strings.ReplaceAll(body, " ", ""), "display:") {
				declaresDisplay = true
			}
		}
		if !declaresDisplay {
			continue // nothing beats the UA rule; hidden works unaided.
		}
		guard := sel + "[hidden]"
		idx := strings.Index(css, guard)
		if idx == -1 {
			t.Errorf("%q sets display but has no %q override — the filter would set "+
				"hidden and the element would keep rendering", sel, guard)
			continue
		}
		rule := css[idx:]
		if end := strings.Index(rule, "}"); end != -1 {
			rule = rule[:end]
		}
		if !strings.Contains(strings.ReplaceAll(rule, " ", ""), "display:none") {
			t.Errorf("the %q override must set display:none, got: %q", guard, rule)
		}
	}
}

// The one place untrusted input is written into the DOM at runtime.
//
// ?q= is attacker-controlled: anyone can craft a URL, and the empty state
// echoes the term back. Go cannot run the script, so this test pins the sink
// *shape* — which is where the vulnerability would live — rather than proving
// runtime safety. That the payload lands inert was confirmed separately by
// loading a crafted ?q= in a browser and observing zero injected elements.
func TestEmptyStateTermIsNotAnInjectionSink(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	js := scriptBlock(t, string(out))

	// No API that parses a string as HTML may appear anywhere in the block.
	// A bare textual ban is only sound because the block's comments are
	// deliberately worded to avoid naming these APIs; a comment mentioning
	// one would mask a real call from this check.
	for _, sink := range []string{
		"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write",
		"eval(", "createContextualFragment", "srcdoc",
		// Absent today; listed so they cannot be introduced. new Function is
		// eval by another name, and setHTML parses a string as markup.
		"new Function", "setHTML",
	} {
		if strings.Contains(js, sink) {
			t.Errorf("the script must never build DOM from an HTML string, found %q", sink)
		}
	}
	// The echo itself must be a text assignment.
	if !strings.Contains(js, "emptyTerm.textContent") {
		t.Error("the echoed search term must be written with textContent, " +
			"the only assignment that cannot create an element")
	}
	// A template action inside the script would let harvested or descriptor
	// text become executable code. html/template switches to JS escaping in a
	// script context — a different context with different rules than the HTML
	// escaping the rest of the page relies on — so the project bans actions
	// here outright rather than reasoning about each one.
	//
	// Asserted against the template SOURCE: in the rendered page the action
	// has already been executed, so the braces are gone and this check would
	// silently pass no matter what was interpolated.
	if src := templateScriptBlock(t); strings.Contains(src, "{{") || strings.Contains(src, "}}") {
		t.Error("the script block must stay free of template actions: " +
			"interpolating into JS is a different escaping context than HTML")
	}
}

// A crafted ?q= is echoed into the page, so make the injection attempt itself
// part of the suite: the value must never reach the served HTML as live
// markup, and the sink must remain text-only.
func TestEmptyStateRejectsMarkupInjection(t *testing.T) {
	// The term arrives at runtime from the URL rather than through Render, so
	// the served markup must contain an *empty* sink — no attacker value can
	// be baked in at render time — and the script must fill it as text.
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `<span id="emptyterm"></span>`) {
		t.Error("the term sink must ship empty: nothing from a URL may be " +
			"rendered into the markup, it is filled as text at runtime")
	}

	// A harvested value carrying the same payload must still be escaped, so
	// the empty-state markup cannot be reached through the render path either.
	a := sample()
	a.Packages[0].Description = `<img src=x onerror=alert(1)>`
	out2, err := Render(a)
	if err != nil {
		t.Fatal(err)
	}
	s2 := string(out2)
	if strings.Contains(s2, "<img src=x onerror=alert(1)>") {
		t.Error("markup reached the page live — it must be escaped")
	}
	if !strings.Contains(s2, "&lt;img src=x onerror=alert(1)&gt;") {
		t.Error("expected the payload escaped into entities")
	}
}

// The echoed term is attacker-controlled in both length and content, so the
// element that renders it must be able to break an unbreakable run.
//
// .empty is display:flex with flex-wrap:wrap, which wraps flex ITEMS and does
// nothing for a long token inside one. Measured before this rule existed: a
// 120-character term pushed documentElement.scrollWidth 123px past a 1200px
// viewport, 200 characters 913px past it, and 10,000 characters 97,669px past
// it — a shared ?q= link that makes the whole page scroll sideways. The same
// hazard is already handled for `pre` (long install URLs) in this stylesheet;
// the element rendering untrusted text needs it at least as much.
func TestEmptyStateCanBreakAnUnbreakableTerm(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	css := styleBlock(t, string(out))

	rule := ""
	for _, r := range strings.Split(css, "}") {
		head, body, ok := strings.Cut(r, "{")
		if !ok {
			continue
		}
		for _, part := range strings.Split(head, ",") {
			if strings.TrimSpace(part) == ".empty" {
				rule = body
			}
		}
	}
	if rule == "" {
		t.Fatal("no .empty rule in the stylesheet")
	}
	flat := strings.ReplaceAll(rule, " ", "")
	// overflow-wrap:anywhere, word-break:break-all or word-wrap:break-word all
	// create a break opportunity; assert the property, not one spelling.
	if !strings.Contains(flat, "overflow-wrap:anywhere") &&
		!strings.Contains(flat, "overflow-wrap:break-word") &&
		!strings.Contains(flat, "word-break:break-all") &&
		!strings.Contains(flat, "word-wrap:break-word") {
		t.Errorf(".empty renders an attacker-controlled term and must be able to "+
			"break an unbreakable run, or a long ?q= makes the page scroll "+
			"sideways; flex-wrap does not do this. Got: %q", rule)
	}
}

// ?q= mirroring must replace the current history entry. filter() runs on every
// input event, so pushing would add one history entry per keystroke and the
// back button would no longer return the reader to where they came from.
func TestURLSyncReplacesHistoryRatherThanPushing(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	js := scriptBlock(t, string(out))
	if strings.Contains(js, "pushState") {
		t.Error("the filter must not pushState: it runs per keystroke and would " +
			"bury the previous page under one history entry per character")
	}
	if !strings.Contains(js, "replaceState") {
		t.Error("the search term must be mirrored into the URL with replaceState, " +
			"so a filtered view can be shared and survives reload")
	}
	// Read back on load, or the shared URL renders an unfiltered page.
	if !strings.Contains(js, "searchParams.get('q')") {
		t.Error("?q= must be read back on load to prime the filter")
	}

	// The next two assertions are scoped to syncURL's own body. Asserting them
	// against the whole script block made both unfailable: "searchParams" was
	// satisfied by the read-back call above, and "catch" by the load-time
	// guard at the bottom of the script.
	sync := jsFunctionBody(t, js, "syncURL")

	// The parameter must be written through URLSearchParams, which percent-
	// encodes the term. Assigning a concatenated string instead silently
	// truncates any term containing &: "a&b" would be written as q=a&b and
	// read back as "a", losing everything after the ampersand.
	if !strings.Contains(sync, "searchParams.set") {
		t.Error("syncURL must write the term with searchParams.set so it is " +
			"encoded; concatenating it into the query string corrupts any " +
			"term containing & or #")
	}
	if strings.Contains(sync, "u.search =") || strings.Contains(sync, "u.search=") {
		t.Error("syncURL must not assign a hand-built query string: that is the " +
			"concatenation searchParams.set exists to avoid")
	}

	// A file:// document has an opaque origin where a replaceState call
	// carrying a URL can throw. Unguarded, that exception escapes the input
	// handler and kills filtering on every keystroke — worse than the defect
	// being fixed. The guard must be inside syncURL, wrapping the throwing
	// call, not merely somewhere in the script.
	if !strings.Contains(sync, "catch") {
		t.Error("syncURL's own body must catch: on an opaque (file://) origin a " +
			"replaceState call can throw, and an unguarded throw here breaks " +
			"filtering entirely")
	}
	if !strings.Contains(sync, "replaceState") {
		t.Error("the replaceState call must sit inside syncURL, so the catch " +
			"above actually guards it")
	}
}

// An unavailable source renders a stub card with no id, which the filter never
// touches, so it reports zero visible cards. A "hide the section when all its
// cards are hidden" rule is therefore vacuously true for it, and would delete
// the "could not be read" disclosure as soon as anyone typed a character
// (spec §7: an unavailable source is an unknown unknown and must stay
// visible). The guard is that a section needs at least one filterable card
// before it is eligible to hide.
func TestUnavailableSourceStubIsNotFilterable(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	// Locate the unavailable source's section and confirm its stub card has
	// no id, keeping it outside the filter's .card[id] set.
	i := strings.Index(s, "could not be")
	if i == -1 {
		t.Fatal("the unavailable-source stub is missing from the page")
	}
	secStart := strings.LastIndex(s[:i], `<section class="src">`)
	if secStart == -1 {
		t.Fatal("the unavailable-source stub is not inside a source section")
	}
	secEnd := strings.Index(s[secStart:], "</section>")
	if secEnd == -1 {
		t.Fatal("unterminated source section")
	}
	sec := s[secStart : secStart+secEnd]
	if strings.Contains(sec, `class="card unavailable" id=`) || strings.Contains(sec, "<div class=\"card unavailable\" id") {
		t.Error("the unavailable stub must not carry an id: that would put it in " +
			"the filter's card set and let a search hide the disclosure")
	}

	// The script must gate section hiding on there being a filterable card.
	js := scriptBlock(t, string(out))
	if !strings.Contains(js, "own.length > 0") {
		t.Error("section hiding must require at least one filterable .card[id]; " +
			"without that guard a source with only an unavailable stub hides " +
			"vacuously and its disclosure disappears when filtering")
	}
}

// --- Fixture built from real build.Build output ---
//
// A hand-rolled model.Atlas can quietly diverge from what the producer
// actually emits. This exercises the real pipeline (a real git repo, a real
// descriptor, internal/build.Build) so the renderer is tested against a
// shape that actually occurs: real warnings of both kinds, a real duplicate
// collision, and a real resolved SHA — not values a renderer author guessed.

func newFixtureRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.test",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.test",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-m", "fixture", "--no-gpg-sign")
	return "file://" + dir
}

func writeDescriptor(t *testing.T, body string) *descriptor.Descriptor {
	t.Helper()
	p := filepath.Join(t.TempDir(), "d.yml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := descriptor.Load(p)
	if err != nil {
		t.Fatalf("descriptor.Load: %v", err)
	}
	return d
}

func TestRenderRealBuildOutputHasBothWarningKinds(t *testing.T) {
	// A duplicate skill named "dup" present at both the walk root and under
	// .claude/ triggers a real duplicate-primitive warning (mirrors
	// internal/harvest's TestWalkReportsDuplicateAcrossBases). An exclude
	// pattern that matches nothing triggers a real unused-exclude warning.
	repo := newFixtureRepo(t, map[string]string{
		"skills/dup/SKILL.md":         "---\nname: dup\ndescription: from root\n---\nbody",
		".claude/skills/dup/SKILL.md": "---\nname: dup\ndescription: from dotclaude\n---\nbody",
	})
	d := writeDescriptor(t, `
company: acme
sources:
  - kind: repo
    name: r
    url: `+repo+`
    exclude:
      - "skills/typo-pattern-*"
`)
	a, err := build.Build(build.Options{
		Descriptor: d,
		Now:        func() string { return "2026-08-18T00:00:00Z" },
		WorkDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("build.Build: %v", err)
	}
	if len(a.Warnings) != 2 {
		t.Fatalf("got %d warnings from real build output, want 2: %+v", len(a.Warnings), a.Warnings)
	}

	out, err := Render(a)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "unused-exclude") && !strings.Contains(s, `matched nothing`) {
		t.Error("real unused-exclude warning must render")
	}
	if !strings.Contains(s, "duplicate") || !strings.Contains(s, "dup") {
		t.Error("real duplicate-primitive warning must render")
	}
	if len(a.Packages) != 1 || len(a.Packages[0].ResolvedSha) != 40 {
		t.Fatalf("expected one package with a real full-length SHA: %+v", a.Packages)
	}
	if !strings.Contains(s, a.Packages[0].ResolvedSha) {
		t.Error("the real resolved SHA must appear on the page")
	}
}
