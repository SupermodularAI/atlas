package harvest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Defect is one problem found in a primitive's frontmatter.
//
// Reason never quotes the offending text. A description can be commercially
// confidential even when it is malformed, and this output is written to CI logs
// — the same reasoning that makes frontmatterParseError content-free.
type Defect struct {
	Path   string // relative to the linted root
	Reason string
}

// Lint reports every frontmatter defect under root that would stop a primitive
// being listed, or would list it wrongly.
//
// It exists because "does it parse" is not the whole question, and a checker
// that asks only that gives false confidence. Two failure modes shipped
// undetected in this project:
//
//   - An unquoted scalar containing ": " is INVALID YAML. The block fails to
//     parse and the primitive is omitted from the catalog. Loud: Atlas warns.
//
//   - An unquoted scalar containing "#" is VALID YAML. Everything from the "#"
//     is a comment, so the value is SILENTLY TRUNCATED — it parses, it is
//     listed, and nothing reports it. One skill sat at 365 of 488 characters,
//     cut mid-sentence, for weeks.
//
// So Lint compares the PARSED value against the RAW value rather than trusting
// a successful parse. That comparison is the reason this function is not just a
// call to ParseFrontmatter.
//
// Deliberately in internal/harvest and using the same yaml.v3 that
// ParseFrontmatter uses: a gate that disagrees with the consumer at the margins
// is worse than no gate, because it teaches confidence it has not earned.
func Lint(root string) ([]Defect, int, error) {
	var defects []Defect
	checked := 0

	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}

		content, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		block, ok := frontmatterBlock(content)
		if !ok {
			// No frontmatter is not a defect: plenty of markdown in a package
			// is documentation, not a primitive.
			return nil
		}
		checked++

		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			rel = p
		}
		rel = filepath.ToSlash(rel)

		var doc map[string]any
		if uerr := yaml.Unmarshal(block, &doc); uerr != nil {
			defects = append(defects, Defect{rel,
				"frontmatter does not parse as YAML — an unquoted value containing " +
					`": " is read as a key/value separator; quote it`})
			return nil
		}
		if doc == nil {
			defects = append(defects, Defect{rel, "frontmatter is empty or not a mapping"})
			return nil
		}

		for _, key := range []string{"name", "description"} {
			raw, present := rawScalar(block, key)
			if !present {
				continue
			}
			got, isStr := doc[key].(string)
			if !isStr {
				defects = append(defects, Defect{rel,
					fmt.Sprintf("%s did not parse as a string", key)})
				continue
			}
			if strings.TrimSpace(got) == "" {
				defects = append(defects, Defect{rel,
					fmt.Sprintf("%s is present but empty", key)})
				continue
			}
			if len(got) < len(raw) {
				defects = append(defects, Defect{rel, fmt.Sprintf(
					"%s is silently truncated: %d of %d characters — an unquoted "+
						`"#" starts a YAML comment; quote the value`,
					key, len(got), len(raw))})
			}
		}
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("lint %s: %w", root, err)
	}

	sort.Slice(defects, func(i, j int) bool {
		if defects[i].Path != defects[j].Path {
			return defects[i].Path < defects[j].Path
		}
		return defects[i].Reason < defects[j].Reason
	})
	return defects, checked, nil
}

// frontmatterBlock returns the bytes between the opening and closing "---".
func frontmatterBlock(content []byte) ([]byte, bool) {
	s := string(content)
	// Tolerate a UTF-8 BOM: an editor can add one and it is not the author's
	// mistake to debug.
	s = strings.TrimPrefix(s, "\xef\xbb\xbf")
	if !strings.HasPrefix(s, "---") {
		return nil, false
	}
	end := strings.Index(s[3:], "\n---")
	if end == -1 {
		return nil, false
	}
	return []byte(s[3 : 3+end]), true
}

// rawScalar returns one key's value as written, with surrounding quotes removed
// and escapes undone, so its length is comparable to the parsed value.
//
// Only single-line scalars are handled. A block scalar (| or >) spans lines and
// cannot be truncated by a "#" the way an inline one can, so it is reported as
// absent rather than measured — measuring it would produce a false positive on
// every multi-line description.
func rawScalar(block []byte, key string) (string, bool) {
	for _, line := range strings.Split(string(block), "\n") {
		if !strings.HasPrefix(line, key+":") {
			continue
		}
		v := strings.TrimSpace(line[len(key)+1:])
		if v == "" || v == "|" || v == ">" || strings.HasPrefix(v, "|") || strings.HasPrefix(v, ">") {
			return "", false
		}
		if len(v) > 1 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
			v = v[1 : len(v)-1]
			v = strings.ReplaceAll(v, `\"`, `"`)
			v = strings.ReplaceAll(v, `\\`, `\`)
		} else if len(v) > 1 && strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'") {
			v = v[1 : len(v)-1]
			v = strings.ReplaceAll(v, "''", "'")
		}
		return v, true
	}
	return "", false
}
