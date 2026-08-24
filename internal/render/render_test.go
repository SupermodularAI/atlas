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
	// The constant prefix must survive, keeping the value a fragment. The name
	// itself is now encoded by cardID rather than appearing literally, so assert
	// the property -- every package href is a same-page fragment -- instead of a
	// spelling: a colon in a name can no longer even reach the href.
	hrefs := packageHrefs(t, s)
	if len(hrefs) == 0 {
		t.Fatal("no package hrefs found; the assertion below would be vacuous")
	}
	for _, href := range hrefs {
		if !strings.HasPrefix(href, "#pkg-") {
			t.Errorf("package href %q is not confined behind the #pkg- prefix", href)
		}
	}
	if strings.Contains(strings.ToLower(s), `href="javascript:`) {
		t.Error("the name escaped the prefix and became the URL scheme")
	}
}

// packageHrefs returns every fragment href emitted by the jump index.
func packageHrefs(t *testing.T, page string) []string {
	t.Helper()
	var out []string
	rest := page
	for {
		i := strings.Index(rest, `<a href="#`)
		if i == -1 {
			return out
		}
		rest = rest[i+len(`<a href="`):]
		j := strings.Index(rest, `"`)
		if j == -1 {
			t.Fatal("unterminated href attribute in rendered page")
		}
		out = append(out, rest[:j])
		rest = rest[j:]
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

// --- Hash navigation and anchor landing ---------------------------------
//
// These pin template text, not runtime behaviour: the <script> never executes
// in `go test`, so like TestIndexRowsCanActuallyHide these assert the
// structural precondition a browser needs, and would still pass against a
// handler that was wired up but subtly wrong. The properties they DO pin are
// the ones a refactor silently drops.

// scriptBody returns the text between <script> and </script>, so a test can
// assert on the script region alone. Asserting page-wide would let a match
// anywhere in harvested body text satisfy (or falsely fail) the check.
func scriptBody(t *testing.T, s string) string {
	t.Helper()
	start := strings.Index(s, "<script>")
	end := strings.Index(s, "</script>")
	if start == -1 || end == -1 || end < start {
		t.Fatalf("rendered page missing a well-formed <script>...</script> region")
	}
	return s[start+len("<script>") : end]
}

// Constraint: the script stays entirely static. A {{ }} action inside it would
// interpolate a harvested string into a JavaScript context, where html/template
// applies JS escaping rather than HTML escaping — a different guard with
// different failure modes, and one the rest of this package's escaping tests
// do not cover. Keeping the script template-free means the single HTML
// escaping guard remains the only one that has to be correct.
//
// Reads the TEMPLATE SOURCE, not Render's output. This distinction is the whole
// test: executing the template consumes the braces, so `{{ .Company }}` inside
// the script would arrive in the output as `acme` and an assertion on the
// rendered page could never observe it. Verified by mutation — the same check
// against Render's output passes with a template action deliberately inserted.
func TestScriptContainsNoTemplateActions(t *testing.T) {
	src, err := files.ReadFile("page.gohtml")
	if err != nil {
		t.Fatalf("reading the embedded template source: %v", err)
	}
	body := scriptBody(t, string(src))
	if strings.Contains(body, "{{") || strings.Contains(body, "}}") {
		t.Error("the <script> block must contain no Go template action: harvested " +
			"data may only reach it via textContent from already-escaped DOM")
	}
}

// Constraint: no sink that turns a string back into markup or code. The script
// is allowed to READ harvested text (textContent); it must never write it
// anywhere that would re-parse it.
func TestScriptUsesNoUnsafeSinks(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	body := scriptBody(t, string(out))
	for _, sink := range []string{"innerHTML", "outerHTML", "eval(", "document.write", "insertAdjacentHTML", "Function("} {
		if strings.Contains(body, sink) {
			t.Errorf("the <script> block must not use %q — harvested text must never be re-parsed as markup or code", sink)
		}
	}
}

// A jump-index link scrolled to the card but left its <details> closed, so
// navigating to a package showed none of its primitives, while the search path
// already auto-opened a matching package. Both entry points must be wired: a
// click on an index link fires hashchange, but loading a URL that already
// carries a hash does not, so one listener cannot cover the other.
func TestScriptOpensDetailsForHashTarget(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	body := scriptBody(t, string(out))

	if !strings.Contains(body, "hashchange") {
		t.Error("no hashchange listener: clicking a jump-index link would scroll to a " +
			"package and leave its primitives collapsed")
	}
	// The handler must also run once at load, otherwise a pasted or bookmarked
	// deep link lands on a closed card.
	//
	// Anchored to a bare statement-position call, not just the name: the string
	// "openHashTarget()" also occurs in the function's own DEFINITION
	// ("function openHashTarget() {"), so a plain Contains check is satisfied by
	// declaring the function and never calling it. Verified by mutation —
	// deleting the load-time call left that weaker form passing.
	reg := strings.Index(body, "window.addEventListener('hashchange'")
	if reg == -1 {
		t.Fatal("expected the hashchange listener registered on window")
	}
	if !strings.Contains(body[:reg], "\n  openHashTarget();") {
		t.Error("openHashTarget must also be invoked directly at load: loading a URL " +
			"that already has a hash fires no hashchange event, so the listener " +
			"alone leaves a deep link's card collapsed")
	}

	// It must actually open the details, not merely locate the card. Scoped to
	// the handler body: the pre-existing search filter also contains
	// "d.open = true", so a page-wide check passes even if this handler does
	// nothing at all. Verified by mutation — gutting the handler left the
	// unscoped form passing on the search code's match.
	fn := strings.Index(body, "function openHashTarget")
	if fn == -1 {
		t.Fatal("expected an openHashTarget function")
	}
	handler := body[fn:]
	if end := strings.Index(handler, "\n  }"); end != -1 {
		handler = handler[:end]
	}
	if !strings.Contains(handler, "d.open = true") {
		t.Errorf("the hash handler must open the target card's <details>, got: %q", handler)
	}
}

// Security constraint: the hash is attacker-influenced text (a package name
// from a marketplace manifest round-trips through it). It must be resolved with
// getElementById, which treats its argument as a literal id, and never handed
// to querySelector, where a name containing a quote or bracket is parsed as
// selector syntax.
func TestScriptResolvesHashWithoutSelectorInjection(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	body := scriptBody(t, string(out))

	if !strings.Contains(body, "document.getElementById(raw)") {
		t.Error("the hash must be resolved via getElementById, which needs no escaping")
	}
	// No querySelector call may take the hash. Scoped to the hash-resolving
	// function: querySelector is legitimate elsewhere with constant selectors.
	fn := body
	if i := strings.Index(body, "function cardForHash"); i != -1 {
		fn = body[i:]
		if j := strings.Index(fn, "\n  }"); j != -1 {
			fn = fn[:j]
		}
	}
	for _, bad := range []string{"querySelector(raw", "querySelector('#' +", `querySelector("#" +`, "querySelector(location", "querySelector('#'+"} {
		if strings.Contains(fn, bad) {
			t.Errorf("the hash reached a selector via %q — a crafted package name would break the selector", bad)
		}
	}
	// The result must be constrained to a card, or a hash naming any other
	// element on the page (#q, #expand) would have its subtree searched.
	if !strings.Contains(fn, "classList.contains('card')") {
		t.Error("the hash target must be confirmed to be a card before it is acted on")
	}
}

// The same package name is escaped twice, differently: html/template gives the
// href URL escaping ("#pkg-my%20pkg") and the id attribute escaping, which
// keeps the literal character ("pkg-my pkg"). location.hash reports the href
// form, so a raw getElementById misses every name containing a space, an
// ampersand, a quote, or any non-ASCII character — the fix would work on an
// ASCII fixture and fail in production. The decode fallback closes that gap,
// and its try/catch is load-bearing: decodeURIComponent throws URIError on a
// malformed escape like "#pkg-%zz", which uncaught would abort the rest of the
// script and take the search filter down with it.
func TestRenderHrefAndIDAreByteIdentical(t *testing.T) {
	a := sample()
	a.Packages[0].Name = "my pkg"
	out, err := Render(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	// The href lands in a URL context and the id in an attribute context, and
	// html/template escapes those differently for every character outside the
	// unreserved set. This USED to diverge -- href="#pkg-my%20pkg" against
	// id="pkg-my pkg" -- and every reader that took one and looked up the other
	// silently found nothing. cardID removes the divergence at the source by
	// emitting only unreserved characters, so the two must now be the same
	// bytes. That equality is the whole guard: assert it directly rather than
	// pinning either spelling, so it holds for any name and any future escaper
	// change.
	wantID := CardID(a.Packages[0].Source, a.Packages[0].Name)
	if !strings.Contains(s, `id="`+wantID+`"`) {
		t.Errorf("expected id=%q in the page", wantID)
	}
	if !strings.Contains(s, `href="#`+wantID+`"`) {
		t.Errorf("expected href=%q in the page", "#"+wantID)
	}
	for _, href := range packageHrefs(t, s) {
		frag := strings.TrimPrefix(href, "#")
		if !strings.Contains(s, `id="`+frag+`"`) {
			t.Errorf("href %q has no byte-identical id: getElementById would return null, "+
				"which is exactly the defect cardID exists to prevent", href)
		}
	}

	// The decode fallback is retained for hashes this page did not emit (an
	// over-encoding mail or chat client), so it should still be there -- but it
	// is no longer what makes a name with a space work.
	body := scriptBody(t, s)
	if !strings.Contains(body, "decodeURIComponent(raw)") {
		t.Error("expected the decode fallback retained for externally re-encoded hashes")
	}

	// Scoped to cardForHash's own body, not the whole script. Unscoped, ANY
	// try/catch anywhere in the script satisfies this — so a refactor that adds
	// an unrelated guard (say around history.replaceState, which throws on
	// file://) while dropping the one around decodeURIComponent would leave the
	// suite green with the guard gone. That is not hypothetical: it is the
	// mutation this assertion is verified against.
	fn := strings.Index(body, "function cardForHash")
	if fn == -1 {
		t.Fatal("expected a cardForHash function")
	}
	resolver := body[fn:]
	if end := strings.Index(resolver, "\n  }"); end != -1 {
		resolver = resolver[:end]
	}
	if !strings.Contains(resolver, "decodeURIComponent") {
		t.Fatalf("the decode must live inside cardForHash; scoping is wrong, got: %q", resolver)
	}
	if !strings.Contains(resolver, "catch") {
		t.Errorf("decodeURIComponent throws URIError on a malformed escape (\"#pkg-%%zz\"); "+
			"uncaught it would abort the script and disable the search filter. "+
			"cardForHash body: %q", resolver)
	}
}

// The jump target landed flush against the viewport edge (measured
// targetTop=0px), reading as clipped rather than as the thing just navigated
// to. scroll-margin-top reserves the gap. Deliberately not a `display`
// declaration: cards are toggled with the hidden attribute by the filter, and
// an author `display` on .card would beat the UA [hidden] rule and silently
// break filtering (see TestIndexRowsCanActuallyHide).
func TestCardHasScrollMarginForAnchorLanding(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(strings.ReplaceAll(s, " ", ""), "scroll-margin-top:") {
		t.Error("no scroll-margin-top: a jump-index target lands flush at the viewport edge")
	}
	// It must be on .card — the element the jump index actually targets.
	idx := strings.Index(s, ".card {")
	if idx == -1 {
		t.Fatal("no .card rule in the stylesheet")
	}
	rule := s[idx:]
	if end := strings.Index(rule, "}"); end != -1 {
		rule = rule[:end]
	}
	if !strings.Contains(strings.ReplaceAll(rule, " ", ""), "scroll-margin-top:") {
		t.Errorf("scroll-margin-top must be on the .card rule (the jump target), got: %q", rule)
	}
}

// Scrolling alone leaves no trace of which card was targeted, so on a dense
// page the reader has to re-find the name they clicked.
func TestCardTargetIsVisuallyAcknowledged(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, ".card:target") {
		t.Error("no :target rule: nothing confirms which card the jump landed on")
	}
	// Anchored to a top-of-line selector ("\n.card:target {"), not a bare
	// substring. The same selector legitimately appears twice: the base rule and
	// the copy nested inside @media (prefers-reduced-motion), indented two
	// spaces. An unanchored search falls through to the nested one when the base
	// rule is deleted, so the test would be satisfied by the accessibility
	// fallback alone and pass with the default-path highlight gone. Verified by
	// mutation — deleting the base rule passed before this anchor was added.
	idx := strings.Index(s, "\n.card:target {")
	if idx == -1 {
		t.Fatal("expected a top-level .card:target rule block (a rule inside a media " +
			"query does not acknowledge the landing for the default path)")
	}
	idx++ // step past the anchoring newline
	rule := s[idx:]
	if end := strings.Index(rule, "}"); end != -1 {
		rule = rule[:end]
	}
	// It must acknowledge visually without touching layout. A `display` here
	// would beat the UA [hidden] rule and break the filter's ability to hide
	// a card (constraint: any display rule on a .hidden-toggled element needs
	// a matching [hidden] override).
	if strings.Contains(strings.ReplaceAll(rule, " ", ""), "display:") {
		t.Error("the :target rule must not set display — .card is toggled with the " +
			"hidden attribute, and an author display would beat the UA [hidden] rule")
	}
	// The acknowledgement may be painted directly by this rule, or delegated to
	// a @keyframes animation it names. Follow the indirection rather than
	// requiring the paint inline: after Finding 1 the ring is drawn by
	// @keyframes (a transition had no usable from-state on :target), so an
	// inline-only check would reject the correct fix.
	paints := func(css string) bool {
		return strings.Contains(css, "outline") || strings.Contains(css, "box-shadow") ||
			strings.Contains(css, "background")
	}
	if !paints(rule) {
		name := ""
		if i := strings.Index(rule, "animation:"); i != -1 {
			fields := strings.Fields(rule[i+len("animation:"):])
			if len(fields) > 0 {
				name = fields[0]
			}
		}
		if name == "" {
			t.Fatalf("the :target rule neither paints an acknowledgement nor names an "+
				"animation that could, got: %q", rule)
		}
		kf := strings.Index(s, "@keyframes "+name)
		if kf == -1 {
			t.Fatalf("the :target rule animates %q but no such @keyframes block exists — "+
				"the highlight would never paint", name)
		}
		block := s[kf:]
		if end := strings.Index(block, "\n}"); end != -1 {
			block = block[:end]
		}
		// The keyframes must actually paint, and must not animate layout: a
		// `display` here would reach the same hidden-toggled .card and beat the
		// UA [hidden] rule, exactly as an inline declaration would.
		if strings.Contains(strings.ReplaceAll(block, " ", ""), "display:") {
			t.Errorf("the @keyframes must not animate display — .card is toggled with the "+
				"hidden attribute, got: %q", block)
		}
		if !paints(block) {
			t.Errorf("@keyframes %s must paint a visible acknowledgement, got: %q", name, block)
		}
	}
}

// Any motion must be disableable by a reader who asked not to be animated —
// and disabling it must not cost them the acknowledgement itself.
//
// The original :target rule used `transition`, which was the Finding-1 defect:
// :target offers no usable from-state, so the highlight ran backwards. The fix
// is a @keyframes animation with explicit endpoints, so this test pins
// `animation: none` as the kill switch. It deliberately checks BOTH halves —
// motion off AND the ring still painted — because an earlier version of the
// reduced-motion path was strictly BETTER than the animated one, which is what
// exposed the animation as the bug. Losing the static fallback would invert
// that failure instead of fixing it.
func TestTargetMotionRespectsReducedMotion(t *testing.T) {
	out, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "animation") && !strings.Contains(s, "transition") {
		return // No motion at all is a valid way to satisfy the requirement.
	}
	if !strings.Contains(s, "prefers-reduced-motion: reduce") {
		t.Fatal("the stylesheet animates but has no prefers-reduced-motion block")
	}
	idx := strings.Index(s, "prefers-reduced-motion: reduce")
	block := s[idx:]
	if end := strings.Index(block, "\n}\n"); end != -1 {
		block = block[:end]
	}
	flat := strings.ReplaceAll(block, " ", "")

	// Half one: the motion is actually switched off, not merely shortened.
	if !strings.Contains(flat, "animation:none") && !strings.Contains(flat, "transition:none") {
		t.Errorf("the reduced-motion block must switch the motion off, got: %q", block)
	}
	// Half two: the acknowledgement survives. Without this, `animation: none`
	// leaves a reduced-motion reader no marker at all — they would be worse
	// off than an animated reader, not calmer.
	if !strings.Contains(flat, "box-shadow:") && !strings.Contains(flat, "outline:") {
		t.Errorf("the reduced-motion block must still paint a static acknowledgement, "+
			"or disabling motion removes the landing marker entirely, got: %q", block)
	}
}
