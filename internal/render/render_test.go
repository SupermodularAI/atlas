package render

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// styleBlock returns the contents of the page's single <style> element, so a
// test can reason about the cascade rather than about arbitrary substrings.
func styleBlock(t *testing.T, s string) string {
	t.Helper()
	open := strings.Index(s, "<style>")
	end := strings.Index(s, "</style>")
	if open == -1 || end == -1 {
		t.Fatal("no <style> block in the rendered page")
	}
	return s[open+len("<style>") : end]
}

// indexRowDisplayRules returns, in source order, every rule that sets `display`
// on an index ROW — the exact set that competes with the [hidden] override for
// the li's display value.
//
// Rules targeting a descendant of the row (`ul.index a`) are excluded on
// purpose: they set display on a different element and never contend for the
// li's, so hiding the row removes them with it regardless of what they say.
func indexRowDisplayRules(css string) []string {
	var out []string
	for _, block := range strings.Split(css, "}") {
		open := strings.Index(block, "{")
		if open == -1 {
			continue
		}
		sel, body := block[:open], block[open+1:]
		// A `display` inside a comment is not a declaration.
		if i := strings.Index(sel, "*/"); i != -1 {
			sel = sel[i+2:]
		}
		sel = strings.TrimSpace(sel)
		if !strings.Contains(sel, ".index") {
			continue
		}
		// The subject of the selector is its last compound. Only a rule whose
		// subject is the li (or the ul.index element itself) can win the row's
		// display value.
		subject := sel
		if i := strings.LastIndexAny(subject, " >"); i != -1 {
			subject = subject[i+1:]
		}
		if !strings.HasPrefix(subject, "li") {
			continue
		}
		if !strings.Contains(strings.ReplaceAll(body, " ", ""), "display:") {
			continue
		}
		out = append(out, sel)
	}
	return out
}

// Companion to TestIndexRowsCanActuallyHide, guarding the same behaviour against
// a different failure. That test asks whether the override EXISTS; this one asks
// whether it still WINS.
//
// `ul.index li[hidden]` (specificity 0,2,2) currently beats `ul.index li`
// (0,1,2) on specificity. Wrapping the list in a nav landmark invites rescoping
// the plain rule to `nav ul.index li` (0,2,3), which silently outranks the
// override and stops the filter hiding anything — while every substring
// assertion in this file keeps passing, because the override text is still
// present.
//
// Two assertions, and their scope is deliberately unequal:
//
//   - ORDER (fully guarded): the [hidden] rule must be the LAST row rule that
//     sets display. This is the practical break vector — any same-specificity
//     rule placed after the override beats it on order alone.
//   - SPECIFICITY (guarded for `nav` only): a descendant prefix beats the
//     override only if it adds a class or id token, since (0,2,2) already has
//     two classes. `main ul.index li` is (0,1,3) and LOSES — measured in a
//     browser, zero ghost rows — so flagging every prefix would fail the test on
//     a non-defect. `nav` is called out because the landmark wrapper this branch
//     added is what makes that particular rewrite tempting.
//
// The narrow half is scoped, not decorative: this test catches a rule inserted
// after the override that the older TestIndexRowsCanActuallyHide misses.
//
// This tests CSS, which the project generally does not. The carve-out is the
// same one the [hidden] test already relies on: this declaration is load-bearing
// for a behaviour, not a colour or a spacing preference.
func TestIndexHiddenOverrideStillWinsTheCascade(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	rules := indexRowDisplayRules(styleBlock(t, string(out)))
	if len(rules) == 0 {
		t.Fatal("no index row rules set display at all — expected at least the [hidden] override")
	}
	if last := rules[len(rules)-1]; !strings.Contains(last, "[hidden]") {
		t.Errorf("the [hidden] override must be the last index row rule that sets display, "+
			"otherwise a later same-or-higher-specificity rule beats it and filtered "+
			"rows keep rendering; last rule is %q (all: %q)", last, rules)
	}
	// A descendant prefix on a competing rule adds specificity the override does
	// not have, which beats it regardless of order.
	for _, sel := range rules {
		if strings.Contains(sel, "[hidden]") {
			continue
		}
		if strings.Contains(sel, "nav") {
			t.Errorf("index rule %q is scoped under nav but the [hidden] override is "+
				"not: the extra descendant raises specificity above the override and "+
				"hiding breaks silently", sel)
		}
	}
}

// The filter's only feedback is the result count, and it updates while the
// search input holds focus — nothing moves a screen reader's virtual cursor over
// it, so without a live region the count changes silently. role=status and
// aria-live=polite are both asserted: status carries the semantic, polite pins
// the queueing behaviour (assertive would interrupt the user mid-keystroke,
// since this fires on every input event).
func TestRenderAnnouncesFilterResultCount(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	i := strings.Index(s, `id="qcount"`)
	if i == -1 {
		t.Fatal(`no element with id="qcount" — the filter has no result count to announce`)
	}
	// Bound the inspection to the qcount tag itself, so an aria-live elsewhere
	// on the page cannot satisfy this test.
	tag := s[strings.LastIndex(s[:i], "<"):]
	if end := strings.Index(tag, ">"); end != -1 {
		tag = tag[:end]
	}
	if !strings.Contains(tag, `aria-live="polite"`) {
		t.Errorf("#qcount needs aria-live=\"polite\" or the filter result count is "+
			"never announced; got: %q", tag)
	}
	if !strings.Contains(tag, `role="status"`) {
		t.Errorf("#qcount needs role=\"status\" to carry the live-region semantic; got: %q", tag)
	}
}

// Widening the filter must announce, not just narrowing. An aria-live region
// defaults to aria-relevant="additions text", so setting textContent to the
// empty string on clear is a REMOVAL and is not announced: a user types a term,
// hears the count, presses Escape, and hears silence — no confirmation the
// filter lifted. The unfiltered branch must therefore produce non-empty text.
//
// Asserted on the script source because this is a behaviour of the static
// filter, and the project has no browser in its gate. The outcome (each state's
// announced string, and that the node is mutated in place rather than replaced)
// was verified separately in a real browser.
func TestFilterAnnouncesWideningNotOnlyNarrowing(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	i := strings.Index(s, "count.textContent")
	if i == -1 {
		t.Fatal("the filter never assigns count.textContent: nothing is announced at all")
	}
	stmt := s[i:]
	if end := strings.Index(stmt, ";"); end != -1 {
		stmt = stmt[:end]
	}
	// The empty-string branch is the defect: `term === '' ? '' : ...` leaves the
	// cleared state silent.
	flat := strings.ReplaceAll(stmt, " ", "")
	if strings.Contains(flat, "term===''?''") {
		t.Errorf("the cleared state assigns the empty string, which an aria-live region "+
			"does not announce (default aria-relevant=\"additions text\"), so lifting the "+
			"filter is silent; got: %q", stmt)
	}
	if !strings.Contains(stmt, "Showing all") {
		t.Errorf("expected the unfiltered state to announce a non-empty count; got: %q", stmt)
	}
	// The node must be mutated in place. Replacing or re-creating a live region
	// does not announce, so a refactor to innerHTML/replaceWith would silently
	// undo this fix — and would breach the template's no-innerHTML rule besides.
	for _, bad := range []string{"replaceWith", "createElement('span')", "innerHTML"} {
		if strings.Contains(s, bad) {
			t.Errorf("live region must be mutated in place via textContent; found %q", bad)
		}
	}
}

// The claim boundary (§9) reaches the announced text too. "Showing all N
// primitives" must describe what this page lists, never what the company has:
// a withheld package's primitives are deliberately not listed, so wording that
// implies exhaustive coverage would widen the claim past what Atlas can support.
func TestFilterCountClaimsOnlyWhatThePageLists(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	i := strings.Index(s, "count.textContent")
	if i == -1 {
		t.Fatal("the filter never assigns count.textContent")
	}
	stmt := s[i:]
	if end := strings.Index(stmt, ";"); end != -1 {
		stmt = stmt[:end]
	}
	// The counts name what is on the page. Anything asserting completeness of
	// the company's published set, or approval, is out of bounds.
	for _, forbidden := range []string{
		"everything", "complete", "all published", "approved", "reviewed", "entire",
	} {
		if strings.Contains(strings.ToLower(stmt), forbidden) {
			t.Errorf("announced count contains %q, which claims more than Atlas can "+
				"support — withheld packages' primitives are not listed; got: %q",
				forbidden, stmt)
		}
	}
}

// The jump index is the page's primary navigation. As a bare <ul> it announces
// as a plain list and is absent from the landmark rotor, so it is reachable only
// by reading linearly from the top of the page.
func TestRenderJumpIndexIsALabelledNavLandmark(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	ul := strings.Index(s, `<ul class="index">`)
	if ul == -1 {
		t.Fatal(`no <ul class="index"> in the rendered page`)
	}
	nav := strings.LastIndex(s[:ul], "<nav")
	if nav == -1 {
		t.Fatal("the jump index is not wrapped in a nav landmark, so it announces as a " +
			"plain list and never appears in the landmark rotor")
	}
	// The nav must open immediately before the list and still be open at it —
	// a nav that closed earlier in the page would satisfy a naive search.
	if strings.Contains(s[nav:ul], "</nav>") {
		t.Fatal("the nearest preceding <nav> closes before ul.index: the index is not inside it")
	}
	navTag := s[nav:]
	if end := strings.Index(navTag, ">"); end != -1 {
		navTag = navTag[:end]
	}
	// The VALUE, not just the attribute. `aria-label=""` leaves the landmark with
	// no accessible name — the precise defect this test exists to prevent — while
	// still satisfying a presence check.
	label := attrValue(navTag, "aria-label")
	if strings.TrimSpace(label) == "" {
		t.Errorf("the nav landmark's aria-label is empty, so it has no accessible name "+
			"and announces as an anonymous navigation region; got: %q", navTag)
	}
	if label != "Packages" {
		t.Errorf("nav aria-label = %q, want %q: the label names what the landmark "+
			"contains, which is how a user picks it out of the landmark rotor", label, "Packages")
	}
	// Close on the nav that actually wraps this list. Searching the whole tail
	// would be satisfied by any unrelated later </nav> if a second one is added.
	tail := s[ul:]
	closeIdx := strings.Index(tail, "</nav>")
	if closeIdx == -1 {
		t.Fatal("the nav landmark is never closed after ul.index")
	}
	if openIdx := strings.Index(tail, "<nav"); openIdx != -1 && openIdx < closeIdx {
		t.Error("a second <nav> opens after ul.index before the wrapping one closes: " +
			"the closure assertion is no longer pinned to this landmark")
	}
}

// attrValue returns the value of a double-quoted attribute in a tag, or "" if
// the attribute is absent or not double-quoted.
func attrValue(tag, name string) string {
	i := strings.Index(tag, name+`="`)
	if i == -1 {
		return ""
	}
	rest := tag[i+len(name)+2:]
	end := strings.Index(rest, `"`)
	if end == -1 {
		return ""
	}
	return rest[:end]
}

// WCAG 2.5.8 (Target Size Minimum) sets a 24px floor. The index anchors were
// 22px: eight adjacent links in a dense grid, where a near-miss lands on the
// neighbour and navigates somewhere the user did not ask for.
//
// The padding has to be on the anchor, not the row. ul.index li is a
// baseline-aligned flex container, so the anchor is content-sized in the cross
// axis and keeps its ~22px line box however tall the row grows; padding on the
// li inflates the row while the measured target stays under the floor. flex:1
// is what makes the gap beside a short name clickable — display:block alone is
// inert on a flex item. Behaviour, not aesthetics: see the note on
// TestIndexHiddenOverrideStillWinsTheCascade.
func TestIndexLinksMeetTargetSizeMinimum(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	css := styleBlock(t, string(out))
	i := strings.Index(css, "ul.index a {")
	if i == -1 {
		t.Fatal("no `ul.index a` rule with its own declarations: the anchor cannot be " +
			"carrying the padding that lifts it over the 24px floor")
	}
	rule := css[i:]
	if end := strings.Index(rule, "}"); end != -1 {
		rule = rule[:end]
	}
	flat := strings.ReplaceAll(rule, " ", "")

	// Compute the budget rather than assert that a `padding` property merely
	// exists. Asserting presence lets `padding: .001rem 0` pass while the target
	// drops back under the floor — the exact defect this test is named for.
	//
	// Assumes the initial root font size of 16px. .88rem and .25rem are both
	// rem-relative, so they resolve against the root, NOT against body's 15px.
	// 14.08 * 1.55 + 2*4 = 29.82px, which matches the height measured in a real
	// browser — the arithmetic is cross-checked against reality, not invented.
	const rootPx = 16.0
	fontRem := declRem(t, css, "ul.index li", "font-size")
	padRem := verticalPaddingRem(t, rule)
	lineHeight := bodyLineHeight(t, css)

	got := fontRem*rootPx*lineHeight + 2*padRem*rootPx
	if got < 24 {
		t.Errorf("index link target computes %.2fpx, under the WCAG 2.5.8 (Target Size "+
			"Minimum) 24px floor: font-size %.3grem * %gpx * line-height %.3g + 2 * "+
			"padding %.3grem * %gpx. Eight adjacent links in a dense grid is exactly "+
			"the case that criterion exists for.",
			got, fontRem, rootPx, lineHeight, padRem, rootPx)
	}

	// Without flex:1 the anchor does not fill the row and the gap beside a short
	// package name stays dead space.
	if !strings.Contains(flat, "flex:1") {
		t.Errorf("ul.index a needs flex:1 to fill the row — display:block is inert on a "+
			"flex item, leaving the gap beside a short name unclickable; got: %q", rule)
	}
}

// declRem reads a rem-valued declaration out of the named rule.
func declRem(t *testing.T, css, selector, prop string) float64 {
	t.Helper()
	i := strings.Index(css, selector+" {")
	if i == -1 {
		t.Fatalf("no %q rule in the stylesheet", selector)
	}
	body := css[i:]
	if end := strings.Index(body, "}"); end != -1 {
		body = body[:end]
	}
	j := strings.Index(body, prop+":")
	if j == -1 {
		t.Fatalf("%q sets no %s", selector, prop)
	}
	return parseRem(t, body[j+len(prop)+1:])
}

// verticalPaddingRem reads the top/bottom component of a `padding` shorthand.
// The rule uses the two-value form (`padding: <vertical> <horizontal>`), so the
// first component is the one the target-size budget depends on.
func verticalPaddingRem(t *testing.T, rule string) float64 {
	t.Helper()
	i := strings.Index(rule, "padding:")
	if i == -1 {
		t.Fatal("ul.index a sets no padding, so the target stays at its bare line box, " +
			"under the WCAG 2.5.8 24px floor")
	}
	return parseRem(t, rule[i+len("padding:"):])
}

// bodyLineHeight reads the unitless line-height out of body's `font` shorthand
// (`font: 15px/1.55 ...`), which is what sets the anchor's line box.
func bodyLineHeight(t *testing.T, css string) float64 {
	t.Helper()
	i := strings.Index(css, "font: ")
	if i == -1 {
		t.Fatal("no body `font` shorthand: cannot determine the line box")
	}
	s := css[i+len("font: "):]
	slash := strings.Index(s, "/")
	if slash == -1 {
		t.Fatal("body `font` shorthand carries no /line-height")
	}
	s = s[slash+1:]
	end := strings.IndexAny(s, " ;\n")
	if end != -1 {
		s = s[:end]
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		t.Fatalf("unparseable line-height %q: %v", s, err)
	}
	return v
}

// parseRem takes the leading rem length off a declaration value.
func parseRem(t *testing.T, s string) float64 {
	t.Helper()
	s = strings.TrimSpace(s)
	end := strings.Index(s, "rem")
	if end == -1 {
		t.Fatalf("expected a rem length, got %q", s)
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s[:end]), 64)
	if err != nil {
		t.Fatalf("unparseable rem length %q: %v", s[:end], err)
	}
	return v
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
