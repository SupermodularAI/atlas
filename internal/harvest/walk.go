package harvest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SupermodularAI/atlas/internal/model"
)

// WalkOptions configures a tree walk. Exclude receives slash-separated paths
// relative to the walk root and reports whether that path is excluded and,
// if so, which pattern matched. Returning ("", false) means "not excluded".
type WalkOptions struct {
	Exclude func(relPath string) (pattern string, excluded bool)
}

// Duplicate records a Type+Name collision between the two bases WalkTree
// reads (root and root/.claude). Type namespaces identity, so this is only
// raised when both Type and Name match — a hook and a skill sharing a name
// is legitimate and never reported.
//
// WalkTree keeps one entry (the root base wins — it is the more specific
// location for a package layout) and reports the rest here rather than
// silently dropping or doubling them, mirroring docs/design.md §6's
// "collisions are reported, never resolved" for the analogous package-level
// case.
type Duplicate struct {
	Type    string // primitive type, e.g. model.TypeSkill
	Name    string
	Kept    string // path of the entry that was retained
	Dropped string // path of the entry that was discarded
}

// WalkTree enumerates primitives in a cloned tree. It looks at the root and at
// root/.claude, so both a package layout and a raw repo layout are recognised
// by a single walker — that is what lets kind: repo reuse this path at no
// extra cost.
//
// The walk root is a hard boundary (docs/design.md §3): every path read,
// however deeply nested, must resolve to somewhere inside root. A symlink
// that escapes it — at a base itself (root/.claude -> elsewhere) or at any
// level beneath (skills/x/SKILL.md -> elsewhere) — fails the walk with an
// error naming the offending path. Fail-closed, not skip-and-continue: a
// silently incomplete harvest is indistinguishable from a complete one, and
// every downstream control (excludes, classification, acknowledgement) only
// operates on paths inside the root, so an escape makes all of them
// irrelevant rather than merely wrong. A symlink that stays inside the root
// is unaffected and continues to resolve normally.
//
// It returns:
//   - the primitives found (a Type+Name collision across the two bases is
//     deduped, root wins, see Duplicate),
//   - the set of exclude patterns that matched at least one path during the
//     walk (deduped, sorted) — lets a caller detect a pattern that is
//     well-formed and legal but withholds nothing, a defect no load-time
//     check can catch,
//   - the set of Type+Name duplicates encountered (deduped, sorted),
//   - an error, non-nil only on a symlink escape or an unreadable/undescribed
//     primitive.
//
// All three slices are empty (never nil) when nothing was found, matched, or
// duplicated: nil means "not harvested" in atlas.json, and a successful walk
// — even of an empty tree, even with nothing to report — must not claim
// that.
func WalkTree(root string, opts WalkOptions) ([]model.Primitive, []string, []Duplicate, error) {
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolve walk root %s: %w", root, err)
	}

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

	// withinRoot verifies p resolves (following any symlinks) to somewhere
	// inside canonicalRoot. Existence is checked first via Lstat: a path
	// that simply does not exist (a base that isn't present, an .mcp.json
	// that was never written) is not an escape and must not be treated as
	// one — EvalSymlinks errors on a nonexistent path.
	withinRoot := func(p string) error {
		if _, err := os.Lstat(p); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("stat %s: %w", p, err)
		}
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", p, err)
		}
		relToRoot, err := filepath.Rel(canonicalRoot, resolved)
		if err != nil {
			return fmt.Errorf("path %s escapes the walk root: %w", p, err)
		}
		if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relToRoot) {
			return fmt.Errorf("path %s escapes the walk root (resolves to %s)", p, resolved)
		}
		return nil
	}

	type accEntry struct {
		prim model.Primitive
		path string
	}
	byKey := map[string]accEntry{} // key: Type+"\x00"+Name, first-seen (root before .claude) wins
	var order []string             // key order, for stable dup reporting
	dupSet := map[string]Duplicate{}

	addFound := func(prim model.Primitive, srcPath string) {
		key := prim.Type + "\x00" + prim.Name
		if existing, ok := byKey[key]; ok {
			dupSet[key] = Duplicate{
				Type:    prim.Type,
				Name:    prim.Name,
				Kept:    existing.path,
				Dropped: srcPath,
			}
			return
		}
		byKey[key] = accEntry{prim: prim, path: srcPath}
		order = append(order, key)
		found = append(found, prim)
	}

	for _, base := range []string{root, filepath.Join(root, ".claude")} {
		if err := withinRoot(base); err != nil {
			return nil, nil, nil, err
		}
		if st, err := os.Stat(base); err != nil || !st.IsDir() {
			continue
		}
		if err := walkBase(root, base, excluded, withinRoot, addFound); err != nil {
			return nil, nil, nil, err
		}
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

	dups := make([]Duplicate, 0, len(dupSet))
	for _, k := range order {
		if d, ok := dupSet[k]; ok {
			dups = append(dups, d)
		}
	}
	sort.Slice(dups, func(i, j int) bool {
		if dups[i].Type != dups[j].Type {
			return dups[i].Type < dups[j].Type
		}
		return dups[i].Name < dups[j].Name
	})

	return found, matched, dups, nil
}

// walkBase enumerates one root (either the tree root or its .claude
// subdirectory) for the recognised layouts. excluded receives paths relative
// to the overall walk root (not to base), so exclude patterns are consistent
// across both layouts. withinRoot is checked before any directory is opened
// or file is read; excluded is checked before withinRoot at the file level so
// an excluded path is never even resolved, preserving "excludes filter
// before any file is read". addFound records a successfully read primitive,
// applying cross-base dedupe.
func walkBase(root, base string, excluded func(relPath string) bool, withinRoot func(string) error, addFound func(model.Primitive, string)) error {
	rel := func(p string) string {
		r, err := filepath.Rel(root, p)
		if err != nil {
			return p
		}
		return filepath.ToSlash(r)
	}

	// skills/<name>/SKILL.md
	skillsDir := filepath.Join(base, "skills")
	if err := withinRoot(skillsDir); err != nil {
		return err
	}
	dirs, err := os.ReadDir(skillsDir)
	if err == nil {
		for _, d := range dirs {
			if !d.IsDir() {
				continue
			}
			dirPath := filepath.Join(skillsDir, d.Name())
			if err := withinRoot(dirPath); err != nil {
				return err
			}
			p := filepath.Join(dirPath, "SKILL.md")
			if excluded(rel(p)) {
				continue
			}
			if err := withinRoot(p); err != nil {
				return err
			}
			prim, err := readDescribed(p, model.TypeSkill, d.Name())
			if err != nil {
				return err
			}
			if prim != nil {
				addFound(*prim, p)
			}
		}
	}

	// agents/<name>.md and commands/<name>.md
	// The directory names are the on-disk convention; the emitted type is the
	// shared vocabulary (ADR-0010). "agents/" holds subagents.
	for dir, typ := range map[string]string{
		"agents":   model.TypeSubagent,
		"commands": model.TypeCommand,
	} {
		subDir := filepath.Join(base, dir)
		if err := withinRoot(subDir); err != nil {
			return err
		}
		entries, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			p := filepath.Join(subDir, e.Name())
			if excluded(rel(p)) {
				continue
			}
			if err := withinRoot(p); err != nil {
				return err
			}
			prim, err := readDescribed(p, typ, strings.TrimSuffix(e.Name(), ".md"))
			if err != nil {
				return err
			}
			if prim != nil {
				addFound(*prim, p)
			}
		}
	}

	// hooks/<file> — scripts carry no frontmatter, so name only. This is not
	// the undescribed-primitive error case: hooks never have a description
	// to be missing, so none is invented and none is required.
	hooksDir := filepath.Join(base, "hooks")
	if err := withinRoot(hooksDir); err != nil {
		return err
	}
	if entries, err := os.ReadDir(hooksDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			p := filepath.Join(hooksDir, e.Name())
			if excluded(rel(p)) {
				continue
			}
			if err := withinRoot(p); err != nil {
				return err
			}
			addFound(model.Primitive{Type: model.TypeHook, Name: e.Name()}, p)
		}
	}

	// .mcp.json
	mcp := filepath.Join(base, ".mcp.json")
	if excluded(rel(mcp)) {
		return nil
	}
	if err := withinRoot(mcp); err != nil {
		return err
	}
	if st, err := os.Stat(mcp); err == nil && !st.IsDir() {
		// .mcp.json declares servers, not individual tools — so the kind is
		// mcp_server. The two stay distinct (ADR-0010): a server is the
		// governable unit, a tool is what fires.
		addFound(model.Primitive{Type: model.TypeMCPServer, Name: ".mcp.json"}, mcp)
	}

	return nil
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
		// Full path, not path.Base: in a package with a dozen skills, "SKILL.md:
		// invalid YAML" names no file an operator can go and fix. The wrapped
		// error is already content-free by construction (see
		// frontmatterParseError), so this adds locality without adding
		// disclosure — a path is not file content.
		return nil, fmt.Errorf("%s: %w", p, err)
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
