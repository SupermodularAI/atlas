// Package build runs the pipeline: resolve, harvest, merge.
package build

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/supermodular/atlas/internal/descriptor"
	"github.com/supermodular/atlas/internal/gitc"
	"github.com/supermodular/atlas/internal/harvest"
	"github.com/supermodular/atlas/internal/model"
	"github.com/supermodular/atlas/internal/resolve"
)

// Warning kinds.
const (
	WarningUnusedExclude      = "unused-exclude"
	WarningDuplicatePrimitive = "duplicate-primitive"
)

// Collision kinds.
const (
	CollisionPackageName   = "package-name"
	CollisionPrimitiveName = "primitive-name"
)

// DetectCollisions finds name clashes across sources and packages. Atlas
// reports clashes; it never resolves them — a package name is only meaningful
// relative to its source, so a union can legitimately hold two of the same name.
func DetectCollisions(pkgs []model.Package) []model.Collision {
	var out []model.Collision

	pkgSources := map[string]map[string]bool{}
	for _, p := range pkgs {
		if pkgSources[p.Name] == nil {
			pkgSources[p.Name] = map[string]bool{}
		}
		pkgSources[p.Name][p.Source] = true
	}
	for name, srcs := range pkgSources {
		if len(srcs) > 1 {
			out = append(out, model.Collision{
				Kind: CollisionPackageName, Name: name, Sources: sortedKeys(srcs),
			})
		}
	}

	// Primitive names clash only across different packages: the same name at
	// two types inside one package is not something a consumer trips over.
	primOwners := map[string]map[string]bool{}
	for _, p := range pkgs {
		for _, prim := range p.Primitives {
			key := prim.Type + ":" + prim.Name
			if primOwners[key] == nil {
				primOwners[key] = map[string]bool{}
			}
			primOwners[key][p.Source+"/"+p.Name] = true
		}
	}
	for key, owners := range primOwners {
		if len(owners) > 1 {
			out = append(out, model.Collision{
				Kind: CollisionPrimitiveName, Name: key, Sources: sortedKeys(owners),
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Options configures a build. Now is injectable so tests get a stable stamp.
type Options struct {
	Descriptor *descriptor.Descriptor
	Now        func() string
	WorkDir    string
}

// Build runs the pipeline and returns the atlas.
//
// Degradation is recorded, never fatal: an unreachable source becomes
// "unavailable" and a denied package becomes "restricted", both visible in the
// output. Only a misconfiguration — a repo source with no classification
// signal and no acknowledgement, or a package whose resolved ref does not
// exist upstream — aborts the run.
func Build(opts Options) (*model.Atlas, error) {
	if opts.Descriptor == nil {
		return nil, fmt.Errorf("build: descriptor is required")
	}
	now := opts.Now
	if now == nil {
		return nil, fmt.Errorf("build: Now is required")
	}
	work := opts.WorkDir
	if work == "" {
		var err error
		if work, err = os.MkdirTemp("", "atlas-work-"); err != nil {
			return nil, fmt.Errorf("build: work dir: %w", err)
		}
		defer os.RemoveAll(work)
	}

	a := &model.Atlas{
		SchemaVersion: model.SchemaVersion,
		Company:       opts.Descriptor.Company,
		GeneratedAt:   now(),
		Sources:       []model.Source{},
		Packages:      []model.Package{},
		Collisions:    []model.Collision{},
		Warnings:      []model.Warning{},
		Summary: model.Summary{
			Sources:  map[string]int{model.StatusRead: 0, model.StatusUnavailable: 0},
			Packages: map[string]int{"harvested": 0, "restricted": 0, "excluded": 0},
		},
	}

	for _, src := range opts.Descriptor.Sources {
		var (
			ms       model.Source
			pkgs     []model.Package
			warnings []model.Warning
			err      error
		)
		switch src.Kind {
		case descriptor.KindMarketplace:
			ms, pkgs, warnings, err = buildMarketplace(src, work)
		case descriptor.KindRepo:
			ms, pkgs, warnings, err = buildRepo(src, work)
		default:
			return nil, fmt.Errorf("source %q: unknown kind %q", src.Name, src.Kind)
		}
		if err != nil {
			return nil, err // misconfiguration, not degradation
		}
		a.Sources = append(a.Sources, ms)
		a.Summary.Sources[ms.Status]++
		a.Packages = append(a.Packages, pkgs...)
		a.Warnings = append(a.Warnings, warnings...)
	}

	for _, p := range a.Packages {
		switch p.Access {
		case model.AccessPublic:
			a.Summary.Packages["harvested"]++
		case model.AccessRestricted:
			a.Summary.Packages["restricted"]++
		case model.AccessExcluded:
			a.Summary.Packages["excluded"]++
		}
	}

	if c := DetectCollisions(a.Packages); len(c) > 0 {
		a.Collisions = c
	}
	return a, nil
}

// buildMarketplace resolves a marketplace source: fetch its manifest, then
// harvest each package it names. Excludes are checked BEFORE cloning — for an
// excluded package, no clone, no read, no URL resolution; the bytes must
// never reach the process.
func buildMarketplace(src descriptor.Source, work string) (model.Source, []model.Package, []model.Warning, error) {
	ms := model.Source{Name: src.Name, Kind: src.Kind}

	clone, err := gitc.Clone(src.URL, "", work)
	if err != nil {
		ms.Status = model.StatusUnavailable
		ms.Reason = err.Error()
		return ms, nil, nil, nil
	}
	defer gitc.Cleanup(clone)

	data, err := readManifest(clone.Dir)
	if err != nil {
		ms.Status = model.StatusUnavailable
		ms.Reason = err.Error()
		return ms, nil, nil, nil
	}
	man, err := resolve.ParseManifest(data)
	if err != nil {
		ms.Status = model.StatusUnavailable
		ms.Reason = err.Error()
		return ms, nil, nil, nil
	}

	ms.Status = model.StatusRead
	ms.SourceBase = man.SourceBase
	ms.Owner = man.Owner
	ms.Version = man.Version

	// Unused-exclude accounting: a pattern that matched no package name in
	// this manifest counts, exactly like a repo-mode path glob that matched
	// nothing. Only computed once the manifest is known to have parsed — an
	// unavailable source's package list is an unknown unknown, so no claim
	// about which patterns were "unused" can be made for it.
	matchedExcludes := map[string]bool{}
	for _, pat := range src.Exclude {
		for _, mp := range man.Packages {
			if pat == mp.Name {
				matchedExcludes[pat] = true
				break
			}
		}
	}
	var warnings []model.Warning
	for _, pat := range src.Exclude {
		if !matchedExcludes[pat] {
			warnings = append(warnings, model.Warning{
				Kind:   WarningUnusedExclude,
				Source: src.Name,
				Detail: fmt.Sprintf("exclude pattern %q matched nothing", pat),
			})
		}
	}

	var pkgs []model.Package
	for _, mp := range man.Packages {
		p := model.Package{
			Name:        mp.Name,
			Source:      src.Name,
			Description: mp.Description,
			Version:     mp.Version,
		}

		if src.IsExcluded(mp.Name) {
			p.Access = model.AccessExcluded
			p.Reason = "excluded by descriptor"
			p.Primitives = nil
			pkgs = append(pkgs, p)
			continue
		}

		url, err := man.ResolveURL(mp)
		if err != nil {
			// A malformed/unresolvable source is a limit on what Atlas could
			// read, not a configuration abort: the reason is surfaced
			// visibly in p.Reason on the rendered card rather than failing
			// the whole run — "a wrong provenance URL on a governance page
			// is worse than a stated failure" (resolve.ResolveURL).
			p.Access = model.AccessRestricted
			p.Reason = err.Error()
			pkgs = append(pkgs, p)
			continue
		}
		p.ResolvedFrom = url

		ref := man.ResolveRef(mp)
		pc, err := gitc.Clone(url, ref, work)
		if err != nil {
			if errors.Is(err, gitc.ErrRefNotFound) {
				// A missing ref means the repo is readable but misconfigured
				// (typically a tagPattern that resolved to a nonexistent
				// tag) — a configuration error, not an access limit. §7
				// forbids rendering it as a locked/restricted card, which
				// would tell the operator they lack permission when they do
				// not. Abort with the package, the ref, and the tagPattern
				// named, so the operator fixes the manifest rather than
				// being told to request access they already have.
				return ms, nil, nil, fmt.Errorf(
					"package %q: ref %q does not exist upstream (tagPattern %q): %w",
					mp.Name, ref, man.TagPattern, err)
			}
			p.Reason = err.Error()
			if !errors.Is(err, gitc.ErrAccessDenied) {
				p.Reason = "clone failed: " + err.Error()
			}
			p.Access = model.AccessRestricted
			pkgs = append(pkgs, p)
			continue
		}
		p.ResolvedSha = pc.Sha

		prims, _, dups, err := harvest.WalkTree(pc.Dir, harvest.WalkOptions{})
		gitc.Cleanup(pc)
		if err != nil {
			// A symlink escape (or an unreadable/undescribed primitive) is a
			// hard failure for this source: the content came from outside
			// the authorised root, so rendering it "restricted" would be a
			// lie about why it is missing.
			return ms, nil, nil, fmt.Errorf("harvest %s: %w", mp.Name, err)
		}
		warnings = append(warnings, duplicateWarnings(src.Name, dups)...)

		p.Access = model.AccessPublic
		p.Primitives = prims
		if man.SourceBase != "" {
			p.Install = &model.Install{
				MarketplaceAdd: fmt.Sprintf("apm marketplace add %s --name %s", src.URL, src.Name),
				Install:        fmt.Sprintf("apm install %s@%s --target claude", mp.Name, src.Name),
			}
		}
		pkgs = append(pkgs, p)
	}
	return ms, pkgs, warnings, nil
}

// buildRepo resolves a repo source: no manifest, harvest the repo's tree
// directly. Fails closed on the unknown case — a repo with neither a
// classification signal nor descriptor excludes must be acknowledged on the
// record.
func buildRepo(src descriptor.Source, work string) (model.Source, []model.Package, []model.Warning, error) {
	ms := model.Source{Name: src.Name, Kind: src.Kind}

	clone, err := gitc.Clone(src.URL, "", work)
	if err != nil {
		if errors.Is(err, gitc.ErrRefNotFound) {
			return ms, nil, nil, fmt.Errorf("source %q: ref not found: %w", src.Name, err)
		}
		ms.Status = model.StatusUnavailable
		ms.Reason = err.Error()
		return ms, nil, nil, nil
	}
	defer gitc.Cleanup(clone)

	hasClassification := findClassification(clone.Dir) != ""
	if !hasClassification && len(src.Exclude) == 0 && !src.AcknowledgeUnclassified {
		return ms, nil, nil, fmt.Errorf(
			"source %q: repo has no classification file and the descriptor sets no exclude rules; "+
				"add exclude globs or set acknowledgeUnclassified: true to record that rendering "+
				"everything present is intended", src.Name)
	}

	ms.Status = model.StatusRead

	// matchedExcludes accumulates every pattern that matched at least one
	// path, not merely the first. WalkTree's Exclude contract only lets one
	// pattern be credited per call, so double-matching paths are tracked
	// here rather than through WalkTree's own returned slice.
	matchedExcludes := map[string]bool{}
	prims, _, dups, err := harvest.WalkTree(clone.Dir, harvest.WalkOptions{
		Exclude: func(rel string) (string, bool) {
			matched := false
			var first string
			for _, pat := range src.Exclude {
				if (descriptor.Source{Kind: descriptor.KindRepo, Exclude: []string{pat}}).IsExcluded(rel) {
					matchedExcludes[pat] = true
					if !matched {
						first = pat
					}
					matched = true
				}
			}
			return first, matched
		},
	})
	if err != nil {
		return ms, nil, nil, fmt.Errorf("harvest %s: %w", src.Name, err)
	}

	var warnings []model.Warning
	for _, pat := range src.Exclude {
		if !matchedExcludes[pat] {
			warnings = append(warnings, model.Warning{
				Kind:   WarningUnusedExclude,
				Source: src.Name,
				Detail: fmt.Sprintf("exclude pattern %q matched nothing", pat),
			})
		}
	}
	warnings = append(warnings, duplicateWarnings(src.Name, dups)...)

	return ms, []model.Package{{
		Name:         src.Name,
		Source:       src.Name,
		Access:       model.AccessPublic,
		ResolvedFrom: src.URL,
		ResolvedSha:  clone.Sha,
		Primitives:   prims,
	}}, warnings, nil
}

// duplicateWarnings turns harvest.WalkTree's reported Type+Name duplicates
// into warnings[] entries — reporting, not resolution, matching §6's "Atlas
// reports; a resolver decides" for the analogous package-level case.
func duplicateWarnings(source string, dups []harvest.Duplicate) []model.Warning {
	var out []model.Warning
	for _, d := range dups {
		out = append(out, model.Warning{
			Kind:   WarningDuplicatePrimitive,
			Source: source,
			Detail: fmt.Sprintf("duplicate %s %q: kept %s, dropped %s", d.Type, d.Name, d.Kept, d.Dropped),
		})
	}
	return out
}

// readManifest finds the marketplace manifest in a cloned marketplace repo.
func readManifest(dir string) ([]byte, error) {
	for _, rel := range []string{
		"apm.yml",
		filepath.Join("apm-catalog", "apm.yml"),
		"apm.yaml",
	} {
		if b, err := os.ReadFile(filepath.Join(dir, rel)); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("no apm.yml found in marketplace repo")
}

// findClassification looks for a classification file Atlas can obey. Atlas
// never classifies; it only honours a classification already written down.
func findClassification(dir string) string {
	for _, rel := range []string{
		"profiles.json",
		filepath.Join("os-dist", "profiles.json"),
		filepath.Join(".claude", "profiles.json"),
	} {
		p := filepath.Join(dir, rel)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}
