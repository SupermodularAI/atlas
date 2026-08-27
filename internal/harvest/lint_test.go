package harvest

import (
	"strings"
	"testing"
)

// The loud case: an unquoted value containing ": " is invalid YAML, so the
// primitive is omitted from the catalog entirely.
func TestLintCatchesUnparseableFrontmatter(t *testing.T) {
	root := tree(t, map[string]string{
		"skills/bad/SKILL.md":  "---\nname: bad\ndescription: Use it for x: y\n---\nbody",
		"skills/good/SKILL.md": "---\nname: good\ndescription: \"Fine: quoted\"\n---\nbody",
	})
	defects, checked, err := Lint(root)
	if err != nil {
		t.Fatal(err)
	}
	if checked != 2 {
		t.Errorf("checked %d blocks, want 2", checked)
	}
	if len(defects) != 1 {
		t.Fatalf("want 1 defect, got %d: %+v", len(defects), defects)
	}
	if defects[0].Path != "skills/bad/SKILL.md" {
		t.Errorf("wrong file: %q", defects[0].Path)
	}
	if !strings.Contains(defects[0].Reason, "does not parse") {
		t.Errorf("reason should name the cause, got %q", defects[0].Reason)
	}
}

// THE test. An unquoted "#" is VALID YAML: it parses, the primitive is listed,
// and nothing anywhere reports that the value was cut. A gate that only asks
// "does it parse" passes this file — which is exactly how one shipped truncated
// for weeks. Verified independently: another YAML implementation also accepts
// it as a valid mapping.
func TestLintCatchesSilentTruncation(t *testing.T) {
	root := tree(t, map[string]string{
		"skills/a/SKILL.md": "---\nname: a\ndescription: Posts to #general when overdue\n---\nbody",
	})
	defects, _, err := Lint(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(defects) != 1 {
		t.Fatalf("want the truncation reported, got %d: %+v", len(defects), defects)
	}
	if !strings.Contains(defects[0].Reason, "truncated") {
		t.Errorf("reason must say truncated, got %q", defects[0].Reason)
	}
	// The numbers matter: they tell an author how much was lost.
	if !strings.Contains(defects[0].Reason, " of ") {
		t.Errorf("reason should report parsed-of-raw lengths, got %q", defects[0].Reason)
	}
}

func TestLintCatchesEmptyValues(t *testing.T) {
	root := tree(t, map[string]string{
		"skills/a/SKILL.md": "---\nname: a\ndescription: \"\"\n---\nbody",
	})
	defects, _, err := Lint(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(defects) != 1 || !strings.Contains(defects[0].Reason, "empty") {
		t.Fatalf("want an empty-value defect, got %+v", defects)
	}
}

// Correctly quoted content must pass, including values that contain the very
// characters the gate looks for. A gate that cannot be satisfied is a gate
// people disable.
func TestLintPassesCorrectlyQuotedValues(t *testing.T) {
	root := tree(t, map[string]string{
		"skills/a/SKILL.md": "---\nname: a\ndescription: \"Handles x: y, posts to #chan, and 100% of cases\"\n---\nbody",
		"skills/b/SKILL.md": "---\nname: b\ndescription: 'Single: quoted'\n---\nbody",
		"skills/c/SKILL.md": "---\nname: c\ndescription: Plain and safe\n---\nbody",
		"README.md":         "# not a primitive\n\nno frontmatter here",
	})
	defects, checked, err := Lint(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(defects) != 0 {
		t.Fatalf("correct content must pass, got %+v", defects)
	}
	// The README has no frontmatter and must not be counted or complained about.
	if checked != 3 {
		t.Errorf("checked %d, want 3 (the README is not a primitive)", checked)
	}
}

// A block scalar spans lines and cannot be truncated by a "#" the way an inline
// value can. Measuring its first line against the parsed whole would report a
// false positive on every multi-line description.
func TestLintDoesNotFalsePositiveOnBlockScalars(t *testing.T) {
	root := tree(t, map[string]string{
		"skills/a/SKILL.md": "---\nname: a\ndescription: |\n  A long description\n  over two lines.\n---\nbody",
	})
	defects, _, err := Lint(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(defects) != 0 {
		t.Fatalf("a block scalar must not be flagged, got %+v", defects)
	}
}

// The reason reaches a CI log, so it must never carry the text that failed —
// a description can be confidential even when malformed.
func TestLintReasonCarriesNoFileContent(t *testing.T) {
	const secret = "MERGER-WITH-INITECH"
	root := tree(t, map[string]string{
		"skills/a/SKILL.md": "---\nname: a\ndescription: plan for " + secret + ": phase one\n---\nbody",
		"skills/b/SKILL.md": "---\nname: b\ndescription: notes on " + secret + " #private\n---\nbody",
	})
	defects, _, err := Lint(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(defects) != 2 {
		t.Fatalf("want both files reported, got %d", len(defects))
	}
	for _, d := range defects {
		if strings.Contains(d.Reason, secret) {
			t.Errorf("%s leaked file content: %q", d.Path, d.Reason)
		}
	}
}
