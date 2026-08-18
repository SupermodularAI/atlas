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

	args := []string{"clone", "--depth", "1", "--filter=blob:none", "--no-tags"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, url, dir)

	if out, err := run(dir, args...); err != nil {
		os.RemoveAll(dir)
		if isAccessFailure(out) {
			return nil, fmt.Errorf("%w: %s", ErrAccessDenied, firstLine(out))
		}
		return nil, fmt.Errorf("git clone %s: %v: %s", url, err, firstLine(out))
	}

	sha, err := run(dir, "rev-parse", "HEAD")
	if err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("resolve HEAD for %s: %w", url, err)
	}
	return &CloneResult{Dir: dir, Sha: strings.TrimSpace(sha)}, nil
}

// run executes git with global/system config neutralised, so a developer's
// personal git config cannot change what Atlas reads.
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

// isAccessFailure distinguishes "you may not read this" from a real fault. The
// git binary reports both through exit status, so the message is all we have.
func isAccessFailure(out string) bool {
	s := strings.ToLower(out)
	for _, sig := range []string{
		"authentication failed",
		"permission denied",
		"could not read username",
		"access denied",
		"repository not found",
		"does not appear to be a git repository",
		"not found",
		"403",
		"404",
	} {
		if strings.Contains(s, sig) {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// Cleanup removes a cloned tree.
func Cleanup(r *CloneResult) {
	if r != nil && r.Dir != "" {
		os.RemoveAll(filepath.Clean(r.Dir))
	}
}
