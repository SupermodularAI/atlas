// Package guard holds repo-wide invariants that are not tied to one package.
package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Guarantee test 3: Atlas is an OSS tool and must carry no company specifics.
// Everything company-shaped arrives via the descriptor or a fetched manifest.
func TestNoHardcodedOrgStrings(t *testing.T) {
	banned := []string{
		"smos-", "supermodularai", "supermodular-os",
		"playgrounds/personal", "transformation-stack", "ai-primitives",
		"joni.oliveira",
	}
	roots := []string{"../../internal", "../../cmd"}

	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, ".gohtml") {
				return nil
			}
			// This file necessarily contains the banned strings as data.
			if strings.HasSuffix(p, "guard_test.go") {
				return nil
			}
			body, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			// Scanned line by line, not whole-file, so an import path can be
			// exempted without weakening the banned list. See isModulePath.
			for i, line := range strings.Split(string(body), "\n") {
				if isModulePath(line) {
					continue
				}
				lower := strings.ToLower(line)
				for _, b := range banned {
					if strings.Contains(lower, b) {
						t.Errorf("%s:%d contains hardcoded org string %q — it must come from the descriptor or manifest",
							p, i+1, b)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}

// isModulePath reports whether a line carries this module's own import path
// rather than a company-specific value.
//
// The distinction §2 actually draws is between *structure* and *behaviour*. A Go
// module path is unavoidably the repository URL, which is unavoidably the
// organisation hosting it — there is no way to publish a module without that
// string appearing in every file importing a sibling package, and it changes
// nothing about what the tool does or who it works for.
//
// A company name in a *value* is the real defect: a hardcoded sourceBase, a
// package-name prefix, a namespace. Those make the tool work for one company
// only, which is what portability forbids — and what this guard caught twice in
// test fixtures on its first run.
//
// So the exemption is deliberately narrow: the banned substring must appear
// inside this module's own path, on a line using that path as an import or a
// module declaration. A bare mention anywhere else still fails.
func isModulePath(line string) bool {
	const modulePath = "github.com/SupermodularAI/atlas"
	trimmed := strings.TrimSpace(line)
	if !strings.Contains(trimmed, modulePath) {
		return false
	}
	// `module <path>` covers go.mod; a quoted path covers both a bare import
	// line inside a block and a named import.
	return strings.HasPrefix(trimmed, "module ") ||
		strings.Contains(trimmed, `"`+modulePath)
}
