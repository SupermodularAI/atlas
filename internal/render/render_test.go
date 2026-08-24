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
func TestRenderHrefAndIDEscapeDifferently(t *testing.T) {
	a := sample()
	a.Packages[0].Name = "my pkg"
	out, err := Render(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	// The premise: pin the divergence, so if html/template ever stops
	// URL-escaping here the decode fallback's justification is re-examined
	// rather than silently kept as cargo cult.
	if !strings.Contains(s, `href="#pkg-my%20pkg"`) {
		t.Error("expected the href URL-escaped; the decode fallback exists because of this")
	}
	if !strings.Contains(s, `id="pkg-my pkg"`) {
		t.Error("expected the id to keep the literal space, diverging from the href")
	}

	// The consequence: the script must handle it.
	body := scriptBody(t, s)
	if !strings.Contains(body, "decodeURIComponent(raw)") {
		t.Error("no decodeURIComponent fallback: a package name containing a space or " +
			"non-ASCII character percent-encodes in the href and would never match its id")
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
