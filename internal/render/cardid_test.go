package render

import (
	"strings"
	"testing"

	"github.com/SupermodularAI/atlas/internal/model"
)

// collisionSample is the fixture that would have caught both defects cardID
// fixes, and that no existing fixture could: two sources publishing the SAME
// package name, plus names carrying every character class that made the two
// escapers disagree.
//
// Kept separate from sample() deliberately. sample() is asserted against by
// count in a dozen tests, so growing it to cover this would have meant editing
// all of them — the sort of friction that leaves a gap uncovered. Note also
// that sample() declares a "package-name" collision in Collisions while having
// no colliding packages, so the notice rendered and the cards never did: the
// collision path was documented but never exercised.
func collisionSample() *model.Atlas {
	pkg := func(name, source, desc string) model.Package {
		return model.Package{
			Name: name, Source: source, Description: desc,
			Access: model.AccessPublic, ResolvedSha: "deadbeef",
			Primitives: []model.Primitive{
				{Type: model.TypeSkill, Name: name + "-prim", Description: "In " + source + "."},
			},
		}
	}
	return &model.Atlas{
		SchemaVersion: model.SchemaVersion,
		Company:       "acme",
		GeneratedAt:   "2026-08-24T00:00:00Z",
		Sources: []model.Source{
			{Name: "mkt", Kind: "marketplace", Status: model.StatusRead},
			{Name: "other", Kind: "marketplace", Status: model.StatusRead},
		},
		Packages: []model.Package{
			// The collision: same name, two sources. Both are listed by design.
			pkg("dup", "mkt", "FIRST from mkt."),
			pkg("dup", "other", "SECOND from other."),
			// Characters on which the URL escaper and the attribute escaper
			// disagreed. Each one silently broke the index-row lookup.
			pkg("data ops", "mkt", "Space."),
			pkg("café", "mkt", "Non-ASCII."),
			pkg("a&b", "mkt", "Ampersand."),
			pkg("a_b", "mkt", "Underscore: the separator, so it must be escaped."),
			pkg("a-b", "other", "Hyphen: safe, must pass through."),
		},
		Collisions: []model.Collision{
			{Kind: "package-name", Name: "dup", Sources: []string{"mkt", "other"}},
		},
		Summary: model.Summary{
			Sources:  map[string]int{"read": 2},
			Packages: map[string]int{"harvested": 7},
		},
	}
}

// D2: two packages sharing a name must get DIFFERENT ids. Before cardID they
// got the same one, getElementById returned the first, and the second package
// was unreachable by hash — clicking its index row scrolled to, targeted and
// expanded the wrong card, with nothing on screen to say so.
func TestCardIDDistinguishesSameNameAcrossSources(t *testing.T) {
	a := collisionSample()
	first := CardID(a.Packages[0].Source, a.Packages[0].Name)
	second := CardID(a.Packages[1].Source, a.Packages[1].Name)
	if first == second {
		t.Fatalf("both %q packages got id %q: the second is unreachable by hash", "dup", first)
	}

	out, err := Render(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, id := range []string{first, second} {
		if n := strings.Count(s, `id="`+id+`"`); n != 1 {
			t.Errorf("id %q appears %d times; a DOM id must be unique", id, n)
		}
	}
}

// Every id the page emits must be unique, whatever the packages are called.
// This is the general form of the assertion above.
func TestRenderEmitsNoDuplicateCardIDs(t *testing.T) {
	out, err := Render(collisionSample())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	rest := string(out)
	for {
		i := strings.Index(rest, `<div class="card`)
		if i == -1 {
			break
		}
		rest = rest[i+1:]
		j := strings.Index(rest, `id="`)
		k := strings.Index(rest, ">")
		if j == -1 || j > k {
			continue // a card without an id (the unavailable-source stub)
		}
		tail := rest[j+len(`id="`):]
		e := strings.Index(tail, `"`)
		if e == -1 {
			t.Fatal("unterminated id attribute")
		}
		seen[tail[:e]]++
	}
	if len(seen) == 0 {
		t.Fatal("no card ids found; the assertion below would be vacuous")
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("duplicate card id %q (%d occurrences)", id, n)
		}
	}
}

// D1: the href fragment and the id must be the SAME BYTES for every package,
// because the filter reads one and looks up the other with no decoding in
// between. A name containing a space used to make those two differ, the lookup
// returned null, and the index row stayed painted while its card was hidden.
func TestRenderFragmentMatchesIDForHostileNames(t *testing.T) {
	out, err := Render(collisionSample())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	hrefs := packageHrefs(t, s)
	if len(hrefs) == 0 {
		t.Fatal("no package hrefs found; the assertion below would be vacuous")
	}
	for _, href := range hrefs {
		frag := strings.TrimPrefix(href, "#")
		if !strings.Contains(s, `id="`+frag+`"`) {
			t.Errorf("fragment %q has no byte-identical id: getElementById(%q) "+
				"returns null, so this row can never be hidden by the filter", href, frag)
		}
	}
}

// The encoding must be injective: distinct (source, name) pairs must never
// collide. The pairs below are the ones a lossy or naively-joined scheme gets
// wrong — a slug that drops unsafe characters maps "data ops" and "data-ops"
// together, and a bare "-" join maps ("a","b-c") onto ("a-b","c").
func TestCardIDIsInjective(t *testing.T) {
	pairs := [][2]string{
		{"a", "b"},
		{"a-b", "c"}, {"a", "b-c"},
		{"a_b", "c"}, {"a", "b_c"},
		{"data ops", "x"}, {"data-ops", "x"}, {"data_ops", "x"}, {"dataops", "x"},
		{"mkt", "dup"}, {"other", "dup"},
		{"café", "x"}, {"cafe", "x"},
		{"a&b", "c"}, {"a", "&bc"},
		{"", "ab"}, {"ab", ""},
		{"A", "b"}, {"a", "B"},
		// Fixed-width escapes are what keep _XX self-delimiting. Drop the
		// leading digit for bytes below 0x10 and "\x01" + "F" encodes as _1F,
		// colliding with the single byte "\x1f". Found by mutation: the table
		// above passed happily with variable-width hex.
		{"x", "\x01F"}, {"x", "\x1f"},
	}
	seen := map[string][2]string{}
	inputs := map[[2]string]bool{}
	for _, p := range pairs {
		if inputs[p] {
			t.Fatalf("pair (%q,%q) listed twice: identical inputs must give identical "+
				"ids, so a duplicate entry would report a collision that is not one", p[0], p[1])
		}
		inputs[p] = true
		id := CardID(p[0], p[1])
		if prev, dup := seen[id]; dup {
			t.Errorf("collision: (%q,%q) and (%q,%q) both give %q",
				prev[0], prev[1], p[0], p[1], id)
		}
		seen[id] = p
	}
}

// A card id must contain only unreserved URI characters. That is the property
// the byte-identity above rests on: those are exactly the characters both
// escapers pass through untouched, verified by rendering each class through
// both contexts.
func TestCardIDUsesOnlyUnreservedCharacters(t *testing.T) {
	const safe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	for _, in := range [][2]string{
		{"mkt", "data ops"}, {"mkt", "café"}, {"mkt", "a&b"}, {"mkt", "a_b"},
		{"mkt", `a"b`}, {"mkt", "a/b:c?d#e"}, {"mkt", "javascript:alert(1)"},
		{"mkt", "<img src=x onerror=alert(1)>"}, {"mkt", "\x00\x1f"},
	} {
		id := CardID(in[0], in[1])
		for i := 0; i < len(id); i++ {
			if !strings.ContainsRune(safe, rune(id[i])) {
				t.Errorf("CardID(%q,%q) = %q contains unsafe byte %q at %d",
					in[0], in[1], id, id[i], i)
			}
		}
	}
}

// The prefix is what stops a package name from ever beginning the URL, which
// is what keeps a name like "javascript:alert(1)" inert in an href. cardID
// must not drop it.
func TestCardIDKeepsTheConstantPrefix(t *testing.T) {
	for _, in := range [][2]string{
		{"mkt", "javascript:alert(1)"}, {"", ""}, {"s", "n"},
	} {
		if got := CardID(in[0], in[1]); !strings.HasPrefix(got, "pkg-") {
			t.Errorf("CardID(%q,%q) = %q lost the pkg- prefix", in[0], in[1], got)
		}
	}
}
