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
//
// Marketplace is a pointer, not a value, so its zero value ("the key was
// never decoded") is distinguishable from "the key was present and decoded
// to an empty block" — see ParseManifest.
type rawManifest struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Marketplace *struct {
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
//
// A document with no marketplace content at all — empty, "null", a
// comment-only body, or whitespace — is rejected: it is indistinguishable
// from a truncated fetch and must not silently render as "this marketplace
// publishes nothing" (design.md §7 forbids collapsing those two states). A
// present `marketplace:` block whose `packages:` list is empty is a
// different, legitimate state — a real marketplace that currently publishes
// nothing — and still parses. The two are told apart by decoding
// Marketplace as a pointer: nil means the key was never present in the
// document; non-nil means it was present, however sparse its contents.
func ParseManifest(data []byte) (*Manifest, error) {
	var raw rawManifest
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, &manifestParseError{err: err}
	}
	if raw.Marketplace == nil {
		return nil, fmt.Errorf("parse manifest: no marketplace content found")
	}
	packages := raw.Marketplace.Packages
	if packages == nil {
		// The marketplace block is present (checked above) but carries no
		// packages: key. Normalise to a non-nil empty slice — a present,
		// legitimately empty package list, distinct from the "marketplace
		// absent" case already rejected above and unambiguous to callers
		// that range over Packages.
		packages = []ManifestPackage{}
	}
	m := &Manifest{
		Name:       raw.Name,
		Owner:      raw.Marketplace.Owner.Name,
		Version:    raw.Version,
		SourceBase: strings.TrimRight(raw.Marketplace.SourceBase, "/"),
		TagPattern: raw.Marketplace.Build.TagPattern,
		Packages:   packages,
	}
	for i, p := range m.Packages {
		if strings.TrimSpace(p.Name) == "" {
			return nil, fmt.Errorf("manifest: packages[%d]: name is required", i)
		}
	}
	return m, nil
}

// manifestParseError wraps a yaml.Unmarshal failure. yaml.Unmarshal's own
// error text quotes a fragment of the offending value (e.g. "cannot
// unmarshal !!str `just-a-...`"), which would echo part of the manifest's
// content into any surface that renders this error (§9/§10). Error()
// therefore names the failure without the underlying message; Unwrap()
// still exposes it so callers can use errors.Is/As.
type manifestParseError struct {
	err error
}

func (e *manifestParseError) Error() string {
	return "parse manifest: invalid YAML"
}

func (e *manifestParseError) Unwrap() error {
	return e.err
}

// fullyQualifiedSchemes are the URL schemes Atlas recognises as a
// self-sufficient clone URL, used as-is without sourceBase. "file" is
// included because fixtures and the e2e gate (AGENTS.md → Testing strategy)
// resolve packages against real git repos over file://.
var fullyQualifiedSchemes = []string{"https://", "http://", "ssh://", "git://", "file://"}

// ResolveURL turns a package's source into a clone URL.
//
// A bare source (no scheme, no "git@" prefix) is the normal form for
// deeply-nested namespaces, because the default "<owner>/<repo>" form
// accepts exactly two path segments while real repos can live four segments
// deep. Such a source resolves only as sourceBase + "/" + source, which is
// why a bare source without a sourceBase is an error rather than a guess: a
// wrong provenance URL on a governance page is worse than a stated failure.
//
// Scheme detection is a prefix check against a fixed allowlist plus the
// "git@" SSH shorthand, not a substring search: a source containing "://"
// anywhere other than at the very start (a mid-string or embedded
// scheme-looking substring) is not a URL, and passing it through as-is would
// produce whatever nonsense string was given. scp-style sources
// ("host:path", no "://") are outside the two source forms design.md §6
// documents and are rejected explicitly rather than silently concatenated
// into a garbage double-path URL — a malformed source must surface as a
// named error, not fail later as an apparent access problem.
func (m *Manifest) ResolveURL(p ManifestPackage) (string, error) {
	src := strings.TrimSpace(p.Source)
	if src == "" {
		src = p.Name
	}
	if strings.HasPrefix(src, "git@") {
		return src, nil
	}
	for _, scheme := range fullyQualifiedSchemes {
		if strings.HasPrefix(src, scheme) {
			return src, nil
		}
	}
	if strings.Contains(src, "://") {
		return "", fmt.Errorf("package %q has a source %q with an unrecognised URL scheme", p.Name, src)
	}
	if colon := strings.Index(src, ":"); colon >= 0 {
		if slash := strings.Index(src, "/"); slash < 0 || colon < slash {
			return "", fmt.Errorf("package %q has an scp-style source %q, which Atlas does not resolve", p.Name, src)
		}
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
