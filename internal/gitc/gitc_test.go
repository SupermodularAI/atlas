package gitc

import (
	"errors"
	"os"
	"path/filepath"
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
