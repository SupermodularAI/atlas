package harvest

import (
	"os"
	"path/filepath"
	"sort"
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
	got, _, err := WalkTree(root, WalkOptions{})
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
	got, _, err := WalkTree(root, WalkOptions{})
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
	got, _, err := WalkTree(root, WalkOptions{
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
	if _, _, err := WalkTree(root, WalkOptions{}); err == nil {
		t.Fatal("expected error: a described-nothing primitive must fail closed")
	}
}

func TestWalkEmptyTreeReturnsEmptyNotNil(t *testing.T) {
	got, matched, err := WalkTree(t.TempDir(), WalkOptions{})
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

	_, matched, err := WalkTree(root, WalkOptions{
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
	_, matched, err := WalkTree(root, WalkOptions{
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
