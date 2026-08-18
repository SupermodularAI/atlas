package gitc

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloneDefaultBranchRecordsSha(t *testing.T) {
	url := NewFixtureRepo(t, map[string]string{"README.md": "hi"}, "")
	res, err := Clone(url, "", t.TempDir())
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if len(res.Sha) != 40 {
		t.Errorf("Sha = %q, want a 40-char SHA", res.Sha)
	}
	if _, err := os.Stat(filepath.Join(res.Dir, "README.md")); err != nil {
		t.Errorf("cloned tree missing README.md: %v", err)
	}
}

func TestCloneAtTag(t *testing.T) {
	url := NewFixtureRepo(t, map[string]string{"a.txt": "x"}, "v1.2.3")
	res, err := Clone(url, "v1.2.3", t.TempDir())
	if err != nil {
		t.Fatalf("Clone at tag: %v", err)
	}
	if len(res.Sha) != 40 {
		t.Errorf("Sha = %q, want a 40-char SHA", res.Sha)
	}
}

func TestCloneMissingTagFails(t *testing.T) {
	url := NewFixtureRepo(t, map[string]string{"a.txt": "x"}, "v1.0.0")
	if _, err := Clone(url, "v9.9.9", t.TempDir()); err == nil {
		t.Fatal("expected error cloning a nonexistent tag")
	}
}

func TestCloneNonexistentRepoIsAccessDenied(t *testing.T) {
	_, err := Clone("file:///nonexistent-"+t.Name(), "", t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrAccessDenied) {
		t.Errorf("err = %v, want ErrAccessDenied", err)
	}
}

func TestCloneNonexistentRepoReasonOmitsTempPath(t *testing.T) {
	_, err := Clone("file:///nonexistent-"+t.Name(), "", t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if strings.Contains(msg, "Cloning into") {
		t.Errorf("err = %q, want reason to omit the temp-dir boilerplate line", msg)
	}
	if !strings.Contains(msg, "does not appear to be a git repository") &&
		!strings.Contains(msg, "does not exist") &&
		!strings.Contains(msg, "not found") {
		t.Errorf("err = %q, want reason to contain the actual git failure text", msg)
	}
}

func TestFailureReasonPrefersFatalLine(t *testing.T) {
	out := "Cloning into 'out'...\n" +
		"fatal: '/tmp/atlas-clone-537204601/nope' does not appear to be a git repository\n" +
		"fatal: Could not read from remote repository.\n"
	got := failureReason(out)
	if strings.Contains(got, "Cloning into") {
		t.Errorf("failureReason(%q) = %q, want boilerplate line excluded", out, got)
	}
	if !strings.Contains(got, "does not appear to be a git repository") {
		t.Errorf("failureReason(%q) = %q, want the fatal: line's content", out, got)
	}
}

func TestFailureReasonFallsBackToLastNonEmptyLine(t *testing.T) {
	out := "Cloning into 'out'...\n" +
		"remote: something went wrong\n" +
		"\n"
	got := failureReason(out)
	if got != "remote: something went wrong" {
		t.Errorf("failureReason(%q) = %q, want last non-empty line", out, got)
	}
}

func TestCloneArgsTerminatesOptionsBeforePositionals(t *testing.T) {
	args := cloneArgs("-weird-url", "-weird-ref", "-weird-dir")

	dashIdx := -1
	for i, a := range args {
		if a == "--" {
			dashIdx = i
			break
		}
	}
	if dashIdx == -1 {
		t.Fatalf("cloneArgs(...) = %v, want a \"--\" option terminator", args)
	}
	for _, positional := range []string{"-weird-url", "-weird-dir"} {
		found := false
		for i := dashIdx + 1; i < len(args); i++ {
			if args[i] == positional {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("cloneArgs(...) = %v, want %q to appear after \"--\"", args, positional)
		}
	}
}

func TestFailureReasonFallsBackToWholeOutput(t *testing.T) {
	out := "single line, no fatal prefix"
	got := failureReason(out)
	if got != out {
		t.Errorf("failureReason(%q) = %q, want whole trimmed output", out, got)
	}
}
