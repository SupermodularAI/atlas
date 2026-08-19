package harvest

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/supermodular/atlas/internal/model"
)

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func names(ps []model.Primitive) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Type+":"+p.Name)
	}
	sort.Strings(out)
	return out
}

func TestWalkFindsAllPrimitiveTypes(t *testing.T) {
	root := tree(t, map[string]string{
		"skills/code-review/SKILL.md": "---\nname: code-review\ndescription: d1\n---\nbody",
		"agents/reviewer.md":          "---\nname: reviewer\ndescription: d2\n---\nbody",
		"commands/deploy.md":          "---\nname: deploy\ndescription: d3\n---\nbody",
		"hooks/guard.sh":              "#!/bin/sh\necho hi",
		".mcp.json":                   `{"mcpServers":{}}`,
	})
	got, _, _, err := WalkTree(root, WalkOptions{})
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}
	want := []string{"agent:reviewer", "command:deploy", "hook:guard.sh", "mcp:.mcp.json", "skill:code-review"}
	if g := names(got); len(g) != len(want) {
		t.Fatalf("got %v, want %v", g, want)
	}
}

func TestWalkFindsDotClaudeLayout(t *testing.T) {
	root := tree(t, map[string]string{
		".claude/skills/a/SKILL.md": "---\nname: a\ndescription: d\n---\nbody",
	})
	got, _, _, err := WalkTree(root, WalkOptions{})
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}
	if len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("got %v, want one skill named a", names(got))
	}
}

func TestWalkAppliesExclude(t *testing.T) {
	root := tree(t, map[string]string{
		"skills/finance-ops/SKILL.md": "---\nname: finance-ops\ndescription: secret\n---\nbody",
		"skills/code-review/SKILL.md": "---\nname: code-review\ndescription: fine\n---\nbody",
	})
	got, _, _, err := WalkTree(root, WalkOptions{
		Exclude: func(rel string) (string, bool) {
			if rel == "skills/finance-ops/SKILL.md" {
				return "skills/finance-ops/*", true
			}
			return "", false
		},
	})
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}
	for _, p := range got {
		if p.Name == "finance-ops" {
			t.Fatal("excluded primitive must not be returned")
		}
	}
	if len(got) != 1 {
		t.Fatalf("got %v, want only code-review", names(got))
	}
}

func TestWalkSkillMissingDescriptionIsError(t *testing.T) {
	root := tree(t, map[string]string{
		"skills/a/SKILL.md": "---\nname: a\n---\nbody",
	})
	if _, _, _, err := WalkTree(root, WalkOptions{}); err == nil {
		t.Fatal("expected error: a described-nothing primitive must fail closed")
	}
}

func TestWalkEmptyTreeReturnsEmptyNotNil(t *testing.T) {
	got, matched, dups, err := WalkTree(t.TempDir(), WalkOptions{})
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}
	if got == nil {
		t.Fatal("empty tree must return an empty slice, not nil — nil means 'not harvested'")
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", names(got))
	}
	if matched == nil {
		t.Fatal("matched-exclude set must be an empty slice, not nil, when nothing matched")
	}
	if len(matched) != 0 {
		t.Fatalf("got matched %v, want empty", matched)
	}
	if dups == nil {
		t.Fatal("duplicates must be an empty slice, not nil, when nothing duplicated")
	}
	if len(dups) != 0 {
		t.Fatalf("got duplicates %v, want empty", dups)
	}
}

// TestWalkReportsMatchedExcludePatterns is the Additional-requirement test:
// two patterns, one that matches a real path and one that matches nothing.
// The report must distinguish them — only the pattern that actually caused an
// exclusion is returned, not merely evaluated patterns, and not unused ones.
func TestWalkReportsMatchedExcludePatterns(t *testing.T) {
	root := tree(t, map[string]string{
		"skills/finance-ops/SKILL.md": "---\nname: finance-ops\ndescription: secret\n---\nbody",
		"skills/code-review/SKILL.md": "---\nname: code-review\ndescription: fine\n---\nbody",
	})
	const matchingPattern = "skills/finance-ops/*"
	const unusedPattern = "skils/*" // typo'd, well-formed, matches nothing

	_, matched, _, err := WalkTree(root, WalkOptions{
		Exclude: func(rel string) (string, bool) {
			if rel == "skills/finance-ops/SKILL.md" {
				return matchingPattern, true
			}
			return "", false
		},
	})
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}
	if len(matched) != 1 || matched[0] != matchingPattern {
		t.Fatalf("got matched %v, want only %q", matched, matchingPattern)
	}
	for _, m := range matched {
		if m == unusedPattern {
			t.Fatal("a pattern that matched nothing must not be reported as matched")
		}
	}
}

func TestWalkDedupesAndSortsMatchedExcludePatterns(t *testing.T) {
	root := tree(t, map[string]string{
		"skills/a/SKILL.md": "---\nname: a\ndescription: d\n---\nbody",
		"skills/b/SKILL.md": "---\nname: b\ndescription: d\n---\nbody",
	})
	_, matched, _, err := WalkTree(root, WalkOptions{
		Exclude: func(rel string) (string, bool) {
			// Both paths excluded by the SAME pattern: must appear once, sorted.
			return "skills/*", true
		},
	})
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}
	if len(matched) != 1 || matched[0] != "skills/*" {
		t.Fatalf("got %v, want deduped [\"skills/*\"]", matched)
	}
}

// --- Symlink escape (ATLAS-06 fix-loop finding 1) ---

// TestWalkRejectsDotClaudeSymlinkEscape covers the headline case: the
// .claude base itself is a symlink resolving outside the walk root.
func TestWalkRejectsDotClaudeSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "skills", "leak"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "skills", "leak", "SKILL.md"),
		[]byte("---\nname: leak\ndescription: SHOULD NOT BE HARVESTED\n---\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".claude")); err != nil {
		t.Fatal(err)
	}

	got, _, _, err := WalkTree(root, WalkOptions{})
	if err == nil {
		t.Fatalf("expected an error for a .claude base escaping the walk root, got primitives %v", names(got))
	}
	if !strings.Contains(err.Error(), filepath.Join(root, ".claude")) {
		t.Fatalf("error must name the offending path (%s), got: %v", filepath.Join(root, ".claude"), err)
	}
	if strings.Contains(err.Error(), "SHOULD NOT BE HARVESTED") {
		t.Fatal("error must not echo the escaped file's contents")
	}
}

// TestWalkRejectsDeeperSymlinkEscape covers a symlink below the base level —
// a single evil entry inside an otherwise legitimate skills/ directory.
func TestWalkRejectsDeeperSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "leak.md"),
		[]byte("---\nname: leak\ndescription: SHOULD NOT BE HARVESTED\n---\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skills", "evil"), 0o755); err != nil {
		t.Fatal(err)
	}
	evilSkill := filepath.Join(root, "skills", "evil", "SKILL.md")
	if err := os.Symlink(filepath.Join(outside, "leak.md"), evilSkill); err != nil {
		t.Fatal(err)
	}

	got, _, _, err := WalkTree(root, WalkOptions{})
	if err == nil {
		t.Fatalf("expected an error for a deeper symlink escaping the walk root, got primitives %v", names(got))
	}
	if !strings.Contains(err.Error(), evilSkill) {
		t.Fatalf("error must name the offending path (%s), got: %v", evilSkill, err)
	}
	if strings.Contains(err.Error(), "SHOULD NOT BE HARVESTED") {
		t.Fatal("error must not echo the escaped file's contents")
	}
}

// TestWalkFollowsSymlinkWithinRoot proves the fix does not ban symlinks
// wholesale: a symlink whose target stays inside the walk root must keep
// resolving and harvesting normally. The symlink is on the SKILL.md file
// itself (a directory-entry symlink for the containing skills/<name> dir is
// a separate, pre-existing limitation unrelated to this fix: os.DirEntry.IsDir
// reports the entry's own type without following it, so such an entry is
// skipped regardless of whether its target is inside or outside the root).
func TestWalkFollowsSymlinkWithinRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.md"),
		[]byte("---\nname: linked\ndescription: fine\n---\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skills", "linked"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real.md"), filepath.Join(root, "skills", "linked", "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	got, _, _, err := WalkTree(root, WalkOptions{})
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}
	if len(got) != 1 || got[0].Name != "linked" {
		t.Fatalf("got %v, want one skill named linked", names(got))
	}
}

// --- Duplicate Type+Name across bases (ATLAS-06 fix-loop finding 2) ---

// TestWalkReportsDuplicateAcrossBases: the same Type+Name present at both
// root/ and root/.claude/ must yield exactly one primitive (root wins) plus
// a reported duplicate — never silently doubled, never silently dropped.
func TestWalkReportsDuplicateAcrossBases(t *testing.T) {
	root := tree(t, map[string]string{
		"skills/dup/SKILL.md":         "---\nname: dup\ndescription: from root\n---\nbody",
		".claude/skills/dup/SKILL.md": "---\nname: dup\ndescription: from dotclaude\n---\nbody",
	})
	got, _, dups, err := WalkTree(root, WalkOptions{})
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}

	var matches []model.Primitive
	for _, p := range got {
		if p.Type == model.TypeSkill && p.Name == "dup" {
			matches = append(matches, p)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("got %d entries for skill:dup, want exactly 1 (deduped): %v", len(matches), matches)
	}
	if matches[0].Description != "from root" {
		t.Fatalf("root base must win the kept entry, got description %q", matches[0].Description)
	}

	if len(dups) != 1 {
		t.Fatalf("got %d duplicates, want exactly 1: %v", len(dups), dups)
	}
	if dups[0].Type != model.TypeSkill || dups[0].Name != "dup" {
		t.Fatalf("got duplicate %+v, want Type=skill Name=dup", dups[0])
	}
}

// TestWalkSameNameDifferentTypeIsNotADuplicate: Type namespaces identity, so
// a hook and a skill sharing a name is legitimate and must not be reported.
func TestWalkSameNameDifferentTypeIsNotADuplicate(t *testing.T) {
	root := tree(t, map[string]string{
		"skills/x/SKILL.md": "---\nname: x\ndescription: a skill named x\n---\nbody",
		"hooks/x":           "#!/bin/sh\necho hi",
	})
	got, _, dups, err := WalkTree(root, WalkOptions{})
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %v, want both the skill and the hook named x", names(got))
	}
	if len(dups) != 0 {
		t.Fatalf("got duplicates %v, want none — Type namespaces identity", dups)
	}
}

// TestWalkNoDuplicateWhenBasesDistinct: distinct primitives in both bases
// must not trigger a false-positive duplicate report.
func TestWalkNoDuplicateWhenBasesDistinct(t *testing.T) {
	root := tree(t, map[string]string{
		"skills/a/SKILL.md":         "---\nname: a\ndescription: from root\n---\nbody",
		".claude/skills/b/SKILL.md": "---\nname: b\ndescription: from dotclaude\n---\nbody",
	})
	got, _, dups, err := WalkTree(root, WalkOptions{})
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %v, want both distinct primitives", names(got))
	}
	if dups == nil {
		t.Fatal("duplicates must be an empty slice, not nil, when nothing duplicated")
	}
	if len(dups) != 0 {
		t.Fatalf("got duplicates %v, want none", dups)
	}
}
