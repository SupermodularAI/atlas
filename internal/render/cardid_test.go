package render

import (
	"strconv"
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
// collide. This is a property over a space, so it is tested by GENERATING that
// space rather than by hand-picking pairs.
//
// The method matters more than the cases here. A hand-written table shipped a
// version of CardID that was not injective at all: every unsafe character in
// the table sat in the NAME, so the escape-in-source path — which every real
// collision needs — was never exercised, and the test named for the property
// passed on a function that lacked it. Two later gaps were found by mutation
// and closed by appending one more pair, patching symptoms rather than the
// instrument. The alphabet below is chosen for the three ingredients a
// collision needs at once: an unsafe byte in the source, hex-digit characters
// in the name, and asymmetric component lengths.
func TestCardIDIsInjective(t *testing.T) {
	// "_" is the escape introducer, "0"/"2"/"A"/"F" are hex digits that are
	// also safe literals, " " and "," escape, "é"/"ǩ" share a low byte, and
	// "a"/"-" are ordinary safe characters.
	//
	// "\x01" and "\x1f" are here for a reason found the hard way, and they are
	// a PAIR: the ambiguity is that "\x01"+"F" and "\x1f" both encode to _1F
	// under variable-width hex, so a generator needs BOTH bytes to build the two
	// sides. An earlier version of this test was a hand-picked table carrying
	// exactly that pair, added by its own mutation testing; the rewrite to a
	// generator dropped it, no other symbol encoded below 0x10, and the
	// variable-width-hex mutation went from killed to surviving. Adding only the
	// sub-0x10 byte was still not enough — it builds one side and never the
	// other. Replacing a table with a generator is a strict improvement only if
	// the alphabet can express every ambiguity the table's entries encoded,
	// which for a collision means every byte on BOTH sides of it.
	alphabet := []string{"a", "-", "_", "0", "2", "A", "F", " ", ",", "é", "ǩ", "\x01", "\x1f"}

	// Grow strings up to 3 bytes of alphabet symbols on each side: a collision
	// needs one side to spend three characters on an escape while the other
	// spends them on separator-plus-literals, so 3 is the shortest length that
	// can express the failure at all.
	var words []string
	words = append(words, "")
	for _, a := range alphabet {
		words = append(words, a)
		for _, b := range alphabet {
			words = append(words, a+b)
			for _, c := range alphabet {
				words = append(words, a+b+c)
			}
		}
	}

	seen := make(map[string][2]string, len(words)*len(words))
	for _, src := range words {
		for _, name := range words {
			id := CardID(src, name)
			if prev, dup := seen[id]; dup {
				t.Fatalf("collision: (%q,%q) and (%q,%q) both give %q",
					prev[0], prev[1], src, name, id)
			}
			seen[id] = [2]string{src, name}
		}
	}
	if len(seen) < 100000 {
		t.Errorf("only %d pairs tested; the space is too small to be meaningful", len(seen))
	}
	t.Logf("%d distinct (source,name) pairs, %d distinct ids", len(seen), len(seen))
}

// The specific pairs that broke the previous encoding, kept as named
// regressions so a future rewrite cannot quietly reintroduce them. Each needs
// an escape in the SOURCE, which is the ingredient the original table lacked.
func TestCardIDSeparatorCannotBeForged(t *testing.T) {
	for _, c := range []struct {
		aSrc, aName, bSrc, bName string
		why                      string
	}{
		{"a b", "2Cq", "a", "20b,q", "escape in source vs separator+hex literals in name"},
		{"a_b", "5Fq", "a", "5Fb_q", "a literal underscore in the source is enough"},
		{"a ", "0A", "a", "20\n", "minimal form"},
		{"my_marketplace", "x", "my", "5Fmarketplacex", "an underscore in a real source name"},
		// Fixed-width escapes are what keep _XX self-delimiting: drop the
		// leading digit for bytes below 0x10 and "\x01"+"F" encodes as _1F,
		// colliding with the single byte "\x1f". Kept as a NAMED pair as well as
		// in the generator's alphabet, because the name records why the byte
		// matters in a way an alphabet entry cannot.
		{"x", "\x01F", "x", "\x1f", "variable-width hex would collapse both to _1F"},
		{"\x01F", "x", "\x1f", "x", "the same ambiguity in the source component"},
	} {
		x, y := CardID(c.aSrc, c.aName), CardID(c.bSrc, c.bName)
		if x == y {
			t.Errorf("(%q,%q) and (%q,%q) both give %q — %s",
				c.aSrc, c.aName, c.bSrc, c.bName, x, c.why)
		}
	}
}

// Rune-wise escaping would narrow a multi-byte rune to one byte and collide.
// The comment on escapeIDPart calls the byte loop load-bearing; this is what
// makes that claim testable.
func TestCardIDDistinguishesRunesSharingALowByte(t *testing.T) {
	if a, b := CardID("s", "é"), CardID("s", "ǩ"); a == b {
		t.Errorf("U+00E9 and U+01E9 both give %q: escaping is not byte-wise", a)
	}
}

// The join order and the exact escape spelling are part of the URL fragment
// this product emits. A shared link has to keep resolving, so pin the format
// rather than only its character class.
func TestCardIDFormatIsStable(t *testing.T) {
	for _, c := range [3][3]string{
		{"mkt", "dup", "pkg-3-mkt-dup"},
		{"mkt", "café", "pkg-3-mkt-caf_C3_A9"},
		{"a b", "2Cq", "pkg-5-a_20b-2Cq"},
	} {
		if got := CardID(c[0], c[1]); got != c[2] {
			t.Errorf("CardID(%q,%q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

// Injectivity by CONSTRUCTION rather than by search: an invertible function is
// injective, so decoding the id back to its two escaped components proves the
// property for every input at once, not only for the sampled space above.
//
// The decoder is the argument the previous encoding could not make. There, the
// boundary had to be inferred and "_20" had two readings; here the length says
// exactly how many bytes the source occupies, so nothing is guessed. Note what
// the decode does NOT need: it never undoes an escape, because the length counts
// bytes of the already-escaped source.
func TestCardIDIsDecodable(t *testing.T) {
	decode := func(t *testing.T, id string) (string, string) {
		t.Helper()
		if !strings.HasPrefix(id, "pkg-") {
			t.Fatalf("id %q lacks the pkg- prefix", id)
		}
		r := id[len("pkg-"):]
		// The decimal length runs to the FIRST "-": digits can never contain
		// one, so the terminator is unambiguous however many digits there are.
		dash := strings.Index(r, "-")
		if dash == -1 {
			t.Fatalf("id %q has no length terminator", id)
		}
		n, err := strconv.Atoi(r[:dash])
		if err != nil {
			t.Fatalf("id %q has a non-decimal length %q", id, r[:dash])
		}
		r = r[dash+1:]
		if n > len(r) {
			t.Fatalf("id %q claims a %d-byte source but only %d bytes remain", id, n, len(r))
		}
		src := r[:n]
		r = r[n:]
		// The cosmetic separator must sit exactly at the counted boundary.
		if !strings.HasPrefix(r, "-") {
			t.Fatalf("id %q: no separator at the counted boundary (got %q)", id, r)
		}
		return src, r[1:]
	}

	for _, c := range [][2]string{
		{"abc", "x"},
		{"", "x"}, {"x", ""}, {"", ""},
		// digits either side of the boundary: the length must win over reading
		// the source's leading digits as part of it
		{"1", "2"}, {"12", "3"}, {"1", "23"},
		// a source whose escaped form starts or ends with the separator byte
		{"-abc", "x"}, {"abc-", "x"}, {"-", "-"},
		// multi-digit and three-digit lengths
		{strings.Repeat("a", 12), "x"}, {strings.Repeat("a", 100), "x"},
		// escapes on both sides, including the introducer and hex literals
		{"a b", "2Cq"}, {"a", "20b,q"}, {"a_b", "5Fq"}, {"a", "5Fb_q"},
		{"café", "ǩ"}, {"my_marketplace", "x"},
	} {
		id := CardID(c[0], c[1])
		gotSrc, gotName := decode(t, id)
		wantSrc, wantName := escapeIDPart(c[0]), escapeIDPart(c[1])
		if gotSrc != wantSrc || gotName != wantName {
			t.Errorf("CardID(%q,%q) = %q decoded to (%q,%q), want (%q,%q)",
				c[0], c[1], id, gotSrc, gotName, wantSrc, wantName)
		}
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
