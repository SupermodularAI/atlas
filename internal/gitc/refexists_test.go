package gitc

import (
	"strings"
	"testing"
)

// A url beginning with "-" must reach git as an OPERAND, never as a flag.
//
// Both url and ref come from a third-party marketplace manifest. Without "--"
// before the operands, a sourceBase of "--upload-pack=<cmd>" is parsed by git as
// an option and the command runs — remote code execution by way of a YAML file.
// This shipped once: cloneArgs had the separator, RefExists did not.
//
// THE ASSERTION IS ON GIT'S MESSAGE, and that is the whole point. A first version
// of this test checked only that RefExists returned an error, and it PASSED on the
// vulnerable code — because a smuggled flag also fails, just for a different
// reason. Both paths return non-nil; only the message distinguishes them:
//
//	without "--"  fatal: No remote configured to list refs from.
//	                     ^ git consumed the argument as a FLAG, leaving no repo
//	with "--"     fatal: strange pathname '--upload-pack=false' blocked
//	                     ^ git treated it as a PATHNAME and refused it
//
// So the test looks for positive evidence that the value was handled as a
// pathname, rather than for the absence of a side effect — which a test can only
// ever fail to notice.
func TestRefExistsTreatsLeadingDashAsOperand(t *testing.T) {
	for _, hostile := range []string{
		"--upload-pack=false",
		"--help",
		"-x",
	} {
		ok, err := RefExists(hostile, "refs/tags/v1.0.0")
		if ok {
			t.Errorf("%q reported as an existing ref", hostile)
			continue
		}
		if err == nil {
			t.Errorf("%q produced no error at all", hostile)
			continue
		}
		msg := err.Error()
		// The tell that the argument was eaten as a flag: git had no repository
		// left to work with.
		if strings.Contains(msg, "No remote configured") {
			t.Errorf("%q reached git as a FLAG, not an operand — the \"--\" separator "+
				"is missing: %v", hostile, err)
		}
		if strings.Contains(msg, "usage: git ls-remote") {
			t.Errorf("%q made git print its usage, so it was parsed as a flag: %v", hostile, err)
		}
		// Positive evidence: git saw a pathname and refused it.
		if !strings.Contains(msg, "strange pathname") {
			t.Errorf("%q: expected git to reject it as a pathname, got: %v", hostile, err)
		}
	}
}

// The separator must not break the ordinary case: a real repository with a ref
// that does not exist is a clean false, not an error.
func TestRefExistsSeparatorDoesNotBreakNormalUse(t *testing.T) {
	url := NewFixtureRepo(t, map[string]string{"README.md": "x"}, "v0.1.0")
	ok, err := RefExists(url, "refs/tags/v9.9.9")
	if err != nil {
		t.Fatalf("a readable repo with a missing tag must not error: %v", err)
	}
	if ok {
		t.Error("reported a nonexistent tag as present")
	}
}
