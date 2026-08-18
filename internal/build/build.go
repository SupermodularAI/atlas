// Package build runs the pipeline: resolve, harvest, merge.
package build

import (
	"sort"

	"github.com/supermodular/atlas/internal/model"
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
