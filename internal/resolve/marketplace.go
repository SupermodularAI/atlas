// Package resolve turns a descriptor source into a list of packages to harvest.
package resolve

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManifestPackage is one entry in a published marketplace's package list.
type ManifestPackage struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Source      string `yaml:"source"`
	Version     string `yaml:"version"`
}

// Manifest is the subset of a published APM marketplace manifest Atlas reads.
type Manifest struct {
	Name       string
	Owner      string
	Version    string
	SourceBase string
	TagPattern string
	Packages   []ManifestPackage
}

// rawManifest mirrors the on-disk YAML shape. Unknown fields (license,
// dependencies, includes, outputs, ...) are intentionally ignored: this
// manifest is authored by a third party, not the operator running Atlas, so
// being strict here would fail Atlas on valid manifests it doesn't need to
// read in full.
type rawManifest struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Marketplace struct {
		Owner struct {
			Name string `yaml:"name"`
		} `yaml:"owner"`
		SourceBase string `yaml:"sourceBase"`
		Build      struct {
			TagPattern string `yaml:"tagPattern"`
		} `yaml:"build"`
		Packages []ManifestPackage `yaml:"packages"`
	} `yaml:"marketplace"`
}

// ParseManifest reads a published marketplace manifest. It is lenient about
// unknown fields — a marketplace manifest is authored elsewhere and may
// legitimately carry keys Atlas does not read.
func ParseManifest(data []byte) (*Manifest, error) {
	var raw rawManifest
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	m := &Manifest{
		Name:       raw.Name,
		Owner:      raw.Marketplace.Owner.Name,
		Version:    raw.Version,
		SourceBase: strings.TrimRight(raw.Marketplace.SourceBase, "/"),
		TagPattern: raw.Marketplace.Build.TagPattern,
		Packages:   raw.Marketplace.Packages,
	}
	for i, p := range m.Packages {
		if strings.TrimSpace(p.Name) == "" {
			return nil, fmt.Errorf("manifest: packages[%d]: name is required", i)
		}
	}
	return m, nil
}

// ResolveURL turns a package's source into a clone URL.
//
// A bare source (no scheme, no "git@" prefix) is the normal form for
// deeply-nested namespaces, because the default "<owner>/<repo>" form
// accepts exactly two path segments while real repos can live four segments
// deep. Such a source resolves only as sourceBase + "/" + source, which is
// why a bare source without a sourceBase is an error rather than a guess: a
// wrong provenance URL on a governance page is worse than a stated failure.
func (m *Manifest) ResolveURL(p ManifestPackage) (string, error) {
	src := strings.TrimSpace(p.Source)
	if src == "" {
		src = p.Name
	}
	if strings.Contains(src, "://") || strings.HasPrefix(src, "git@") {
		return src, nil
	}
	if m.SourceBase == "" {
		return "", fmt.Errorf("package %q has a relative source %q but the manifest declares no sourceBase", p.Name, src)
	}
	return m.SourceBase + "/" + strings.TrimLeft(src, "/"), nil
}

// ResolveRef returns the git ref to clone, or "" for the default branch.
// Atlas never requires a tag: reproducibility comes from recording the
// resolved SHA, not from demanding a tag exist.
func (m *Manifest) ResolveRef(p ManifestPackage) string {
	if m.TagPattern == "" || p.Version == "" {
		return ""
	}
	return strings.ReplaceAll(m.TagPattern, "{version}", p.Version)
}
