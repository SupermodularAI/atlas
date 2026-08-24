package render

import (
	"strconv"
	"strings"
)

// CardID returns the DOM id (and, with a "#" prefix, the fragment) that
// identifies one package card on the page.
//
// Two properties are required of it, and both were defects before it existed:
//
// INJECTIVE over (source, name). A package name is only unique within its
// source — the page's own collisions notice says so and deliberately lists
// both — so keying an id on the name alone gave two cards the same id.
// getElementById returns the first, and the second package became unreachable
// by hash: clicking its index row scrolled to, targeted and expanded the wrong
// card, with no signal to the reader that the name they clicked was not the
// card they got.
//
// INVARIANT under both of html/template's escapers. The id lands in an
// attribute context (htmlescaper) while the fragment lands in a URL context
// (urlescaper), and the two disagree on every character outside the unreserved
// set: "data ops" became id="pkg-data ops" but href="#pkg-data%20ops", and
// "café" became "café" against "caf%c3%a9". Any code that reads one and looks
// up the other then finds nothing. That is not hypothetical — the filter did
// exactly that, so a package whose name contained a space silently kept its
// index row painted while its card was hidden, and the count disagreed with
// what was on screen.
//
// Rather than teach every reader to reconcile the two encodings — the page had
// one such reconciliation already and needed a second — the id is built from
// the unreserved characters only. Both escapers pass those through byte for
// byte, so id and fragment are identical by construction and a plain
// getElementById is correct everywhere.
//
// The form is a LENGTH-PREFIXED pair:
//
//	pkg-<len(esc(source))>-<esc(source)>-<esc(name)>
//
//	("mkt", "dup")   -> pkg-3-mkt-dup
//	("mkt", "café")  -> pkg-3-mkt-caf_C3_A9
//	("a b", "2Cq")   -> pkg-5-a_20b-2Cq
//	("a",   "20b,q") -> pkg-1-a-20b_2Cq
//
// The length prefix is what makes the split point explicit rather than
// inferred, and it is not decoration. An earlier version of this function
// joined the components with a bare "_" and was NOT injective; an adversarial
// review caught it by reproducing two packages sharing one id in a browser.
// The reasoning error is worth recording, because it is the kind that survives
// review: escaping "_" inside the components removes literal underscores, and
// fixed-width hex makes an escape self-delimiting once you know where it
// starts — but neither of those pins WHERE THE SEPARATOR IS. The escape
// introducer is also "_", and every hex digit is itself a safe character, so
// "_20" reads equally well as one escape or as separator + literal "2" +
// literal "0". ("a b","2Cq") and ("a","20b,q") both produced pkg-a_20b_2Cq.
// Self-delimiting is not prefix-free. A length prefix supplies the property
// directly instead of arguing for it.
//
// The length counts bytes of the ESCAPED source, so nothing has to undo an
// escape to find the boundary. It is decimal, terminated by "-": that byte is
// safe under both escapers and cannot occur among the digits, so the prefix
// stays unambiguous at any length.
func CardID(source, name string) string {
	es := escapeIDPart(source)
	// The "-" between the components is COSMETIC, not structural: the length
	// prefix already pins the boundary, so this only keeps a shared link
	// readable (pkg-6-mymkt-mypkg rather than pkg-6-mymktmypkg, where the two
	// component names would otherwise run together). It is counted out of the source
	// length, so it can be any safe byte without touching injectivity — which
	// is exactly what an explicit split buys over an inferred one.
	return "pkg-" + strconv.Itoa(len(es)) + "-" + es + "-" + escapeIDPart(name)
}

// escapeIDPart maps one component into the unreserved set, using "_" as a
// percent-style escape introducer.
//
// Operates on BYTES, not runes: a multi-byte rune becomes one _XX group per
// byte. Doing this rune-wise and narrowing to a single byte would collide —
// "é" (U+00E9) and "ǩ" (U+01E9) share the low byte 0xE9 — so the byte loop is
// load-bearing rather than stylistic.
//
// Hex digits are upper-case and always two wide. The width keeps each escape
// self-delimiting; the case is part of the documented output format, so a
// shared link stays stable across builds.
func escapeIDPart(s string) string {
	var b strings.Builder
	// Most names are already safe, so size for the common case and let the
	// builder grow for the rest.
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '.', c == '~':
			b.WriteByte(c)
		default:
			const hex = "0123456789ABCDEF"
			b.WriteByte('_')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0F])
		}
	}
	return b.String()
}
