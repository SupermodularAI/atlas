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
			lower := strings.ToLower(string(body))
			for _, b := range banned {
				if strings.Contains(lower, b) {
					t.Errorf("%s contains hardcoded org string %q — it must come from the descriptor or manifest", p, b)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}
