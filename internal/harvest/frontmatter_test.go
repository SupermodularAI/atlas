package harvest

import "testing"

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
	if _, _, err := ParseFrontmatter([]byte("---\nname: x\nno closing fence\n")); err == nil {
		t.Fatal("expected error for unterminated frontmatter")
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
