// Package gitc runs the git binary. Shelling out rather than using a library is
// deliberate: the user's existing auth (SSH agent, credential helper, keychain)
// applies unchanged, which is what Atlas's host-agnostic requirement needs.
package gitc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrAccessDenied wraps any clone failure Atlas should treat as "cannot read
// this" rather than a hard error — auth failure, not-found, or a vanished repo.
// Callers render these as access "restricted".
var ErrAccessDenied = errors.New("access denied")

// ErrRefNotFound wraps a clone failure where the repository is readable but
// the requested ref (branch, tag, or SHA) does not exist there. This is a
// config error (e.g. a tagPattern that resolved to a nonexistent tag), not an
// access problem, and callers must not render it as a locked/restricted
// package card — see docs/design.md §7's two-level degradation model.
var ErrRefNotFound = errors.New("ref not found")

// CloneResult is where the tree landed and what commit it actually is.
type CloneResult struct {
	Dir string
	Sha string
}

// Clone shallow-clones url into a new directory under destParent. An empty ref
// clones the default branch. The resolved SHA is always recorded, because
// reproducibility comes from recording what was fetched, not from requiring
// tags.
func Clone(url, ref, destParent string) (*CloneResult, error) {
	dir, err := os.MkdirTemp(destParent, "atlas-clone-")
	if err != nil {
		return nil, fmt.Errorf("mkdir clone dest: %w", err)
	}

	args := cloneArgs(url, ref, dir)

	if out, err := run(dir, args...); err != nil {
		os.RemoveAll(dir)
		// Checked before the broad access scan: a missing ref is a config
		// error, not an access problem, and "not found" would otherwise trip
		// isAccessFailure and misreport it as ErrAccessDenied.
		if isRefNotFound(out) {
			return nil, fmt.Errorf("%w: %s", ErrRefNotFound, failureReason(out))
		}
		if isAccessFailure(out) {
			return nil, fmt.Errorf("%w: %s", ErrAccessDenied, failureReason(out))
		}
		return nil, fmt.Errorf("git clone %s: %v: %s", url, err, failureReason(out))
	}

	sha, err := run(dir, "rev-parse", "HEAD")
	if err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("resolve HEAD for %s: %w", url, err)
	}
	return &CloneResult{Dir: dir, Sha: strings.TrimSpace(sha)}, nil
}

// cloneArgs builds the argv for `git clone`. "--" terminates option parsing
// before the positional url/dir. Not currently exploitable — exec.Command's
// argv model already prevents a leading "-" in url/ref from being reparsed
// as a flag — but url and ref come from third-party manifests, and this is
// free defence in depth.
func cloneArgs(url, ref, dir string) []string {
	args := []string{"clone", "--depth", "1", "--filter=blob:none", "--no-tags"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	return append(args, "--", url, dir)
}

// run executes git with global/system config neutralised, so a developer's
// personal git config cannot change what Atlas reads.
// RefExists reports whether ref resolves at url, without cloning.
//
// A pinned version whose tag does not exist upstream is silent until something
// tries to fetch it — and the reverse is worse: a package tagged upstream but
// not bumped in the manifest leaves the consumer reading the OLD tag,
// successfully, reporting nothing. ls-remote answers both cheaply.
//
// Reuses run(), so the same hardening applies: no credential prompt, and global
// and system git config neutralised.
func RefExists(url, ref string) (bool, error) {
	out, err := run("", "ls-remote", "--exit-code", url, ref)
	if err == nil {
		return true, nil
	}
	// --exit-code makes git exit 2 for "no matching ref", which is an answer,
	// not a failure. Anything else (auth, network, bad URL) is a real error and
	// must not be reported as "the tag is missing".
	if exitCode(err) == 2 || isRefNotFound(out) {
		return false, nil
	}
	return false, fmt.Errorf("ls-remote %s %s: %s", url, ref, failureReason(out))
}

// exitCode extracts a process exit status, or -1 when err is not an ExitError.
func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if _, err := os.Stat(dir); err == nil {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", // never block waiting for credentials
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// isRefNotFound reports whether out is git's message for a ref (branch, tag,
// or SHA) that does not exist on an otherwise-readable repo. This must be
// checked before isAccessFailure: unlike the genuinely ambiguous messages
// isAccessFailure matches, this one is unambiguous — git names the ref and
// says plainly that it wasn't found — so there is no trade being accepted
// here, only a case to detect explicitly.
func isRefNotFound(out string) bool {
	return strings.Contains(strings.ToLower(out), "not found in upstream")
}

// isAccessFailure distinguishes "you may not read this" from a real fault. The
// git binary reports both through exit status, so the message is all we have.
//
// This is deliberately broad — it accepts the trade that a false "restricted"
// is safer than a crash for messages that are genuinely ambiguous. The bare
// "not found" substring used to live in this list too, but it also matched
// git's "Remote branch X not found in upstream origin" (a config error, not
// an access problem) and caused a live misclassification. That case is not
// ambiguous — git names the ref — so it is now matched explicitly by
// isRefNotFound and checked first; "not found" is dropped here rather than
// kept, since ref-not-found was the only realistic source of it and every
// other case in this list already has a more specific signature.
func isAccessFailure(out string) bool {
	s := strings.ToLower(out)
	for _, sig := range []string{
		"authentication failed",
		"permission denied",
		"could not read username",
		"access denied",
		"repository not found",
		"does not appear to be a git repository",
		"403",
		"404",
	} {
		if strings.Contains(s, sig) {
			return true
		}
	}
	return false
}

// failureReason picks the most informative line out of git's combined
// output. Git's first line is always the "Cloning into '<dir>'..."
// boilerplate — the actual reason is the first "fatal:"/"error:" line, if
// any. Falling back to the first line (as this used to do) means every
// wrapped error reports a meaningless temp-dir path instead of a reason,
// which is a problem when that text becomes a published "locked" package
// card's reason field (see docs/design.md §7).
func failureReason(s string) string {
	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "fatal:") || strings.HasPrefix(lower, "error:") {
			if i := strings.IndexByte(line, ':'); i >= 0 {
				return strings.TrimSpace(line[i+1:])
			}
			return line
		}
	}

	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}

	return s
}

// Cleanup removes a cloned tree.
func Cleanup(r *CloneResult) {
	if r != nil && r.Dir != "" {
		os.RemoveAll(filepath.Clean(r.Dir))
	}
}
