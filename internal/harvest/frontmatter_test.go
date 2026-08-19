package harvest

import (
	"errors"
	"strings"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	name, desc, err := ParseFrontmatter([]byte(`---
name: code-review
description: Review code changes with a rigorous methodology.
---

# Body text here
`))
	if err != nil {
		t.Fatalf("ParseFrontmatter: %v", err)
	}
	if name != "code-review" {
		t.Errorf("name = %q, want code-review", name)
	}
	if desc != "Review code changes with a rigorous methodology." {
		t.Errorf("description = %q", desc)
	}
}

func TestParseFrontmatterMultilineDescription(t *testing.T) {
	_, desc, err := ParseFrontmatter([]byte(`---
name: x
description: >-
  first line
  second line
---
body
`))
	if err != nil {
		t.Fatalf("ParseFrontmatter: %v", err)
	}
	if desc != "first line second line" {
		t.Errorf("description = %q, want folded scalar joined", desc)
	}
}

func TestParseFrontmatterMissingBlockIsError(t *testing.T) {
	if _, _, err := ParseFrontmatter([]byte("# no frontmatter\n")); err == nil {
		t.Fatal("expected error when frontmatter is absent")
	}
}

func TestParseFrontmatterUnterminatedIsError(t *testing.T) {
	// The body here is valid YAML on its own (verified: yaml.Unmarshal accepts
	// it with no error) so this fixture can only fail via fence detection, not
	// via a YAML parse error — unlike a fixture whose body is invalid YAML,
	// which would fail for the wrong reason regardless of whether the fence
	// check runs at all.
	_, _, err := ParseFrontmatter([]byte("---\nname: x\ndescription: d\n"))
	if err == nil {
		t.Fatal("expected error for unterminated frontmatter")
	}
	if !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("err = %q, want it to mention the unterminated-fence guard", err.Error())
	}
}

func TestParseFrontmatterStripsLeadingBOM(t *testing.T) {
	name, desc, err := ParseFrontmatter([]byte("\xef\xbb\xbf---\nname: x\ndescription: d\n---\nbody\n"))
	if err != nil {
		t.Fatalf("ParseFrontmatter: %v", err)
	}
	if name != "x" || desc != "d" {
		t.Errorf("name=%q desc=%q, want x/d with BOM stripped", name, desc)
	}
}

func TestParseFrontmatterStripsBOMFollowedByWhitespace(t *testing.T) {
	name, desc, err := ParseFrontmatter([]byte("\xef\xbb\xbf  \n---\nname: x\ndescription: d\n---\nbody\n"))
	if err != nil {
		t.Fatalf("ParseFrontmatter: %v", err)
	}
	if name != "x" || desc != "d" {
		t.Errorf("name=%q desc=%q, want x/d with BOM+whitespace stripped", name, desc)
	}
}

func TestParseFrontmatterFenceLikePrefixIsNotAFence(t *testing.T) {
	// "---foo" is not an opening fence: a real fence must be followed
	// immediately by a line break. Assert the specific "no frontmatter block"
	// rejection, not merely "some error" — otherwise this would also pass if
	// the adjacency guard were removed and the input instead fell through to
	// the (also erroring, but differently-worded) unterminated-fence path.
	_, _, err := ParseFrontmatter([]byte("---foo\nname: x\n"))
	if err == nil {
		t.Fatal("expected error: a --- not followed by a line break is not an opening fence")
	}
	if !strings.Contains(err.Error(), "no frontmatter block") {
		t.Errorf("err = %q, want it to mention the missing-fence guard", err.Error())
	}
}

func TestParseFrontmatterInvalidYAMLErrorDoesNotLeakContent(t *testing.T) {
	// §9/§10: parse-error text must not echo fragments of a possibly
	// confidential primitive's frontmatter body. yaml.Unmarshal's own
	// type-mismatch errors quote the offending value verbatim (e.g.
	// "cannot unmarshal !!str `just a ...` into harvest.frontmatter") — the
	// rendered message must not pass that quoted fragment through.
	secret := "just-a-secret-value-that-must-not-leak"
	_, _, err := ParseFrontmatter([]byte("---\n" + secret + "\n---\nbody\n"))
	if err == nil {
		t.Fatal("expected error for a bare scalar frontmatter body")
	}
	// yaml.Unmarshal truncates its quoted fragment (observed: "just-a-...");
	// check the truncated prefix rather than the full secret, since that
	// prefix is the actual leaked fragment the rendered message must not
	// contain.
	if strings.Contains(err.Error(), secret[:7]) {
		t.Errorf("err = %q, must not quote a fragment of frontmatter content", err.Error())
	}
	// The underlying yaml error must still be reachable via errors.Unwrap/Is,
	// even though it is kept out of the rendered message.
	if errors.Unwrap(err) == nil {
		t.Error("expected the underlying yaml error to be reachable via errors.Unwrap")
	}
}

func TestParseFrontmatterPreservesMarkupVerbatim(t *testing.T) {
	// Escaping is the renderer's job. The parser must not silently alter input,
	// or the escaping test in the render package would pass for the wrong reason.
	_, desc, err := ParseFrontmatter([]byte(`---
name: x
description: "uses <script>alert(1)</script> internally"
---
body
`))
	if err != nil {
		t.Fatalf("ParseFrontmatter: %v", err)
	}
	if desc != "uses <script>alert(1)</script> internally" {
		t.Errorf("description = %q, want verbatim markup", desc)
	}
}
