package render

import "strings"

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
// The encoding is percent-style with "_" in place of "%", over the safe
// alphabet [A-Za-z0-9.~-]:
//
//	café       -> caf_C3_A9
//	data ops   -> data_20ops
//	a_b        -> a_5Fb
//
// "_" is itself escaped. Leaving it literal would be the whole point missed:
// with "_" as the component separator, an unescaped "_" inside a component
// would let ("a_b", "c") and ("a", "b_c") produce the same id, restoring by a
// side door the collision this function exists to remove.
func CardID(source, name string) string {
	return "pkg-" + escapeIDPart(source) + "_" + escapeIDPart(name)
}

// escapeIDPart maps one component into [A-Za-z0-9.~-] plus "_" escapes.
//
// Operates on BYTES, not runes: a multi-byte rune becomes one _XX group per
// byte, which keeps the mapping total (every possible input encodes) and
// reversible without needing to know where rune boundaries fell.
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
			// Upper-case hex, always two digits: a fixed width keeps the
			// escape self-delimiting, so "_2" followed by a literal "0"
			// cannot be misread as "_20".
			const hex = "0123456789ABCDEF"
			b.WriteByte('_')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0F])
		}
	}
	return b.String()
}
