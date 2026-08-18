package harvest

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/supermodular/atlas/internal/model"
)

// WalkOptions configures a tree walk. Exclude receives slash-separated paths
// relative to the walk root and reports whether that path is excluded and,
// if so, which pattern matched. Returning ("", false) means "not excluded".
type WalkOptions struct {
	Exclude func(relPath string) (pattern string, excluded bool)
}

// WalkTree enumerates primitives in a cloned tree. It looks at the root and at
// root/.claude, so both a package layout and a raw repo layout are recognised
// by a single walker — that is what lets kind: repo reuse this path at no
// extra cost.
//
// It returns the primitives found, and separately the set of exclude
// patterns that matched at least one path during the walk (deduped, sorted).
// That set lets a caller detect a pattern that is well-formed and legal but
// withholds nothing — a defect no load-time check can catch, because only a
// walk over a real tree can observe it.
//
// Both slices are empty (never nil) when nothing was found or nothing
// matched: nil means "not harvested" in atlas.json, and a successful walk —
// even of an empty tree, even with no excludes configured — must not claim
// that.
func WalkTree(root string, opts WalkOptions) ([]model.Primitive, []string, error) {
	found := []model.Primitive{}
	matchedSet := map[string]struct{}{}

	excluded := func(relPath string) bool {
		if opts.Exclude == nil {
			return false
		}
		pattern, ok := opts.Exclude(relPath)
		if ok {
			matchedSet[pattern] = struct{}{}
		}
		return ok
	}

	for _, base := range []string{root, filepath.Join(root, ".claude")} {
		if st, err := os.Stat(base); err != nil || !st.IsDir() {
			continue
		}
		ps, err := walkBase(root, base, excluded)
		if err != nil {
			return nil, nil, err
		}
		found = append(found, ps...)
	}

	sort.SliceStable(found, func(i, j int) bool {
		if found[i].Type != found[j].Type {
			return found[i].Type < found[j].Type
		}
		return found[i].Name < found[j].Name
	})

	matched := make([]string, 0, len(matchedSet))
	for p := range matchedSet {
		matched = append(matched, p)
	}
	sort.Strings(matched)

	return found, matched, nil
}

// walkBase enumerates one root (either the tree root or its .claude
// subdirectory) for the recognised layouts. excluded receives paths relative
// to the overall walk root (not to base), so exclude patterns are consistent
// across both layouts.
func walkBase(root, base string, excluded func(relPath string) bool) ([]model.Primitive, error) {
	var out []model.Primitive

	rel := func(p string) string {
		r, err := filepath.Rel(root, p)
		if err != nil {
			return p
		}
		return filepath.ToSlash(r)
	}

	// skills/<name>/SKILL.md
	dirs, err := os.ReadDir(filepath.Join(base, "skills"))
	if err == nil {
		for _, d := range dirs {
			if !d.IsDir() {
				continue
			}
			p := filepath.Join(base, "skills", d.Name(), "SKILL.md")
			if excluded(rel(p)) {
				continue
			}
			prim, err := readDescribed(p, model.TypeSkill, d.Name())
			if err != nil {
				return nil, err
			}
			if prim != nil {
				out = append(out, *prim)
			}
		}
	}

	// agents/<name>.md and commands/<name>.md
	for dir, typ := range map[string]string{
		"agents":   model.TypeAgent,
		"commands": model.TypeCommand,
	} {
		entries, err := os.ReadDir(filepath.Join(base, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			p := filepath.Join(base, dir, e.Name())
			if excluded(rel(p)) {
				continue
			}
			prim, err := readDescribed(p, typ, strings.TrimSuffix(e.Name(), ".md"))
			if err != nil {
				return nil, err
			}
			if prim != nil {
				out = append(out, *prim)
			}
		}
	}

	// hooks/<file> — scripts carry no frontmatter, so name only. This is not
	// the undescribed-primitive error case: hooks never have a description
	// to be missing, so none is invented and none is required.
	if entries, err := os.ReadDir(filepath.Join(base, "hooks")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			p := filepath.Join(base, "hooks", e.Name())
			if excluded(rel(p)) {
				continue
			}
			out = append(out, model.Primitive{Type: model.TypeHook, Name: e.Name()})
		}
	}

	// .mcp.json
	mcp := filepath.Join(base, ".mcp.json")
	if st, err := os.Stat(mcp); err == nil && !st.IsDir() {
		if !excluded(rel(mcp)) {
			out = append(out, model.Primitive{Type: model.TypeMCP, Name: ".mcp.json"})
		}
	}

	return out, nil
}

// readDescribed reads a frontmatter-bearing primitive. A primitive with no
// description fails the build: the upstream emitter throws rather than ship
// an undescribed package, and Atlas takes the same posture.
func readDescribed(p, typ, fallbackName string) (*model.Primitive, error) {
	content, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	name, desc, err := ParseFrontmatter(content)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path.Base(p), err)
	}
	if strings.TrimSpace(desc) == "" {
		return nil, fmt.Errorf("%s: %s %q has no description — add one before it can appear in an atlas",
			p, typ, fallbackName)
	}
	if strings.TrimSpace(name) == "" {
		name = fallbackName
	}
	return &model.Primitive{Type: typ, Name: name, Description: desc}, nil
}
