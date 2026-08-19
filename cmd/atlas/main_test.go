package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesBothArtifacts(t *testing.T) {
	repo := newFixtureRepo(t, map[string]string{
		".claude/skills/a/SKILL.md": "---\nname: a\ndescription: Does a thing.\n---\nbody",
	}, "")
	dir := t.TempDir()
	desc := filepath.Join(dir, "d.yml")
	if err := os.WriteFile(desc, []byte("company: acme\nsources:\n  - kind: repo\n    name: r\n    url: "+repo+"\n    acknowledgeUnclassified: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "site")

	if err := run(desc, out, false); err != nil {
		t.Fatalf("run: %v", err)
	}

	js, err := os.ReadFile(filepath.Join(out, "atlas.json"))
	if err != nil {
		t.Fatalf("atlas.json: %v", err)
	}
	if !strings.Contains(string(js), `"schemaVersion": 1`) {
		t.Error("atlas.json missing schemaVersion")
	}
	html, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatalf("index.html: %v", err)
	}
	if !strings.Contains(string(html), "Does a thing.") {
		t.Error("index.html missing harvested description")
	}
}

func TestStrictFailsOnDegradation(t *testing.T) {
	dir := t.TempDir()
	desc := filepath.Join(dir, "d.yml")
	if err := os.WriteFile(desc, []byte("company: acme\nsources:\n  - kind: marketplace\n    name: gone\n    url: file:///nonexistent-atlas-strict\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "site")

	if err := run(desc, out, false); err != nil {
		t.Fatalf("non-strict run should succeed: %v", err)
	}
	if err := run(desc, out, true); err == nil {
		t.Fatal("--strict must fail when a source is unavailable")
	}
}

// TestStrictSucceedsOnCleanRun pins the other half of the --strict contract:
// a run with zero unavailable sources, zero restricted packages, and zero
// warnings must succeed under --strict, not merely fail correctly when
// degraded. Without this case, replacing the --strict condition with an
// unconditional failure (every run fails, degraded or not) still passes
// TestStrictFailsOnDegradation and TestStrictFailsOnWarningWithNoDegradation,
// since both of those only assert failure on an already-degraded fixture.
//
// The fixture must be genuinely clean: no exclude entries at all, since an
// exclude that matches nothing emits its own unused-exclude warning and would
// make the run non-clean.
func TestStrictSucceedsOnCleanRun(t *testing.T) {
	repo := newFixtureRepo(t, map[string]string{
		".claude/skills/a/SKILL.md": "---\nname: a\ndescription: Does a thing.\n---\nbody",
	}, "")
	dir := t.TempDir()
	desc := filepath.Join(dir, "d.yml")
	if err := os.WriteFile(desc, []byte("company: acme\nsources:\n  - kind: repo\n    name: r\n    url: "+repo+"\n    acknowledgeUnclassified: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "site")

	if err := run(desc, out, true); err != nil {
		t.Fatalf("--strict must succeed on a clean run (zero unavailable, zero restricted, zero warnings): %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "atlas.json")); err != nil {
		t.Errorf("atlas.json not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "index.html")); err != nil {
		t.Errorf("index.html not written: %v", err)
	}
}

// TestStrictFailsOnWarningWithNoDegradation pins the decision that --strict
// also fails on a recorded warning (e.g. an inert exclude pattern) even when
// every source was read and every package harvested — i.e. zero
// unavailable/restricted counters. Without this case, a --strict that only
// checks unavailable/restricted (ignoring warnings entirely) would still pass
// TestStrictFailsOnDegradation, since that test's fixture has no warnings.
func TestStrictFailsOnWarningWithNoDegradation(t *testing.T) {
	repo := newFixtureRepo(t, map[string]string{
		".claude/skills/a/SKILL.md": "---\nname: a\ndescription: Does a thing.\n---\nbody",
	}, "")
	dir := t.TempDir()
	desc := filepath.Join(dir, "d.yml")
	// exclude pattern matches nothing in the fixture tree -> unused-exclude
	// warning, but the source is still fully read and the package still
	// harvested: no unavailable source, no restricted package.
	if err := os.WriteFile(desc, []byte("company: acme\nsources:\n  - kind: repo\n    name: r\n    url: "+repo+"\n    exclude:\n      - \"nothing-matches-this/*\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "site")

	if err := run(desc, out, false); err != nil {
		t.Fatalf("non-strict run should succeed despite the warning: %v", err)
	}
	if err := run(desc, out, true); err == nil {
		t.Fatal("--strict must fail when a warning was recorded, even with no unavailable/restricted counts")
	}
}

// TestErrorPrintsOnce builds the real binary and runs it against a
// configuration that aborts before anything is written (a repo source with
// no classification file and no acknowledgeUnclassified/exclude), asserting
// the error message appears exactly once on stderr, and that stdout stays
// empty. Before this fix, cobra's own error path (Execute() returning a
// non-nil error with SilenceUsage but not SilenceErrors) printed
// "Error: <msg>", and main()'s own fmt.Fprintln(os.Stderr, "atlas:", err)
// printed the same message again — the exact duplication pr-review
// reproduced on every abort path, not just --strict. This is a subprocess
// test rather than a unit test on run() because the duplication is a
// property of main()'s wiring of cobra's error printing, which only exists
// once Execute() is actually called, and main() offers no injectable writer
// to restructure around without widening the change beyond cmd/atlas's
// existing shape. Stdout and stderr are captured into separate buffers
// (rather than CombinedOutput) because pr-review named "stdout stays 0
// bytes" as a load-bearing pipe-safety property this suite didn't pin.
func TestErrorPrintsOnce(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "atlas")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	repo := newFixtureRepo(t, map[string]string{
		".claude/skills/a/SKILL.md": "---\nname: a\ndescription: Does a thing.\n---\nbody",
	}, "")
	dir := t.TempDir()
	desc := filepath.Join(dir, "d.yml")
	if err := os.WriteFile(desc, []byte("company: acme\nsources:\n  - kind: repo\n    name: r\n    url: "+repo+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "site")

	cmd := exec.Command(bin, "--descriptor", desc, "--out", out)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if runErr == nil {
		t.Fatal("expected the binary to exit non-zero on a fail-closed unclassified repo")
	}

	if stdout.Len() != 0 {
		t.Errorf("expected stdout to stay empty on abort, got %d bytes:\n%s", stdout.Len(), stdout.String())
	}

	const marker = "no classification file"
	n := strings.Count(stderr.String(), marker)
	if n != 1 {
		t.Errorf("expected the error to be printed exactly once on stderr (found %d occurrences of %q); stderr:\n%s", n, marker, stderr.String())
	}
}

// captureStderr redirects os.Stderr for the duration of fn and returns
// everything written to it. run() prints unconditionally to os.Stderr rather
// than an injectable writer, so a real fd swap is the only way to observe its
// output in-process without restructuring run()'s signature. The read side is
// drained on a separate goroutine so fn() can't block on a full pipe buffer.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	done := make(chan string, 1)
	go func() {
		out := make([]byte, 0, 4096)
		tmp := make([]byte, 4096)
		for {
			n, rerr := r.Read(tmp)
			out = append(out, tmp[:n]...)
			if rerr != nil {
				break
			}
		}
		done <- string(out)
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return <-done
}

// TestOutOverwriteNoticed asserts that re-running atlas against an outDir
// that already holds an atlas.json from a previous run prints a note that
// the file was overwritten. Atlas's output is regenerable by construction and
// silent overwrite is the intended contract (no --force, no refusal) — but an
// operator re-running the tool into the same directory should be told their
// previous atlas.json was replaced, not left to notice by diffing it
// themselves.
func TestOutOverwriteNoticed(t *testing.T) {
	repo := newFixtureRepo(t, map[string]string{
		".claude/skills/a/SKILL.md": "---\nname: a\ndescription: Does a thing.\n---\nbody",
	}, "")
	dir := t.TempDir()
	desc := filepath.Join(dir, "d.yml")
	if err := os.WriteFile(desc, []byte("company: acme\nsources:\n  - kind: repo\n    name: r\n    url: "+repo+"\n    acknowledgeUnclassified: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "site")

	if err := run(desc, out, false); err != nil {
		t.Fatalf("first run: %v", err)
	}

	stderr := captureStderr(t, func() {
		if err := run(desc, out, false); err != nil {
			t.Fatalf("second run: %v", err)
		}
	})

	if !strings.Contains(stderr, "atlas.json") || !strings.Contains(stderr, "overwritten") {
		t.Errorf("expected stderr to note atlas.json was overwritten, got:\n%s", stderr)
	}
}
