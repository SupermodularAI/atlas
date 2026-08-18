package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// newFixtureRepo creates a real git repo in a temp dir, writes files, commits,
// and optionally tags. Returns a file:// URL suitable for gitc.Clone.
//
// This mirrors internal/gitc/fixture_test.go's NewFixtureRepo. That helper
// lives in a _test.go file and is therefore not importable across packages,
// so this package needs its own colocated copy rather than depending on an
// unexported test symbol — see AGENTS.md → Testing strategy, which specifies
// this exact pattern (build a real repo in t.TempDir(), clone over file://)
// for any test that needs one.
func newFixtureRepo(t *testing.T, files map[string]string, tag string) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.test",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.test",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-b", "main")
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-m", "fixture", "--no-gpg-sign")
	if tag != "" {
		run("tag", tag)
	}
	return "file://" + dir
}
