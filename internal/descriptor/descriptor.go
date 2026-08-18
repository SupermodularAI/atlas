// Package descriptor loads and validates the company descriptor — Atlas's only
// input. A company has more than one marketplace, so the descriptor, not a URL,
// is the unit Atlas describes.
package descriptor

import (
	"fmt"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// Source kinds.
const (
	KindMarketplace = "marketplace"
	KindRepo        = "repo"
)

// Source is one place Atlas reads primitives from.
type Source struct {
	Kind string `yaml:"kind"`
	Name string `yaml:"name"`
	URL  string `yaml:"url"`

	// Exclude lists things never harvested, rendered as access "excluded".
	// Package names for KindMarketplace; path globs for KindRepo.
	Exclude []string `yaml:"exclude"`

	// AcknowledgeUnclassified permits a repo source that carries neither a
	// classification file nor excludes. Atlas fails closed without it.
	AcknowledgeUnclassified bool `yaml:"acknowledgeUnclassified"`
}

// Descriptor is the parsed company descriptor.
type Descriptor struct {
	Company string   `yaml:"company"`
	Sources []Source `yaml:"sources"`
}

// IsExcluded reports whether name is excluded by this source. Marketplace
// sources exclude by exact package name; repo sources by path glob.
func (s Source) IsExcluded(name string) bool {
	for _, pat := range s.Exclude {
		if s.Kind == KindMarketplace {
			if pat == name {
				return true
			}
			continue
		}
		if matchGlob(pat, name) {
			return true
		}
	}
	return false
}

// matchGlob supports the "**" suffix that path.Match does not, so a pattern
// like "skills/finance-*/**" matches at any depth beneath the prefix.
func matchGlob(pattern, name string) bool {
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		// Match the prefix itself against each ancestor path of name.
		for p := name; p != "." && p != "/" && p != ""; p = path.Dir(p) {
			if ok, _ := path.Match(prefix, p); ok {
				return true
			}
		}
		return false
	}
	ok, _ := path.Match(pattern, name)
	return ok
}

// Load reads, parses and validates a descriptor file.
func Load(p string) (*Descriptor, error) {
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read descriptor: %w", err)
	}
	var d Descriptor
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("parse descriptor: %w", err)
	}
	if err := d.validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

func (d *Descriptor) validate() error {
	if strings.TrimSpace(d.Company) == "" {
		return fmt.Errorf("descriptor: company is required")
	}
	if len(d.Sources) == 0 {
		return fmt.Errorf("descriptor: at least one source is required")
	}
	seen := map[string]bool{}
	for i, s := range d.Sources {
		switch s.Kind {
		case KindMarketplace, KindRepo:
		default:
			return fmt.Errorf("descriptor: sources[%d]: unknown kind %q (want %q or %q)",
				i, s.Kind, KindMarketplace, KindRepo)
		}
		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("descriptor: sources[%d]: name is required", i)
		}
		if strings.TrimSpace(s.URL) == "" {
			return fmt.Errorf("descriptor: sources[%d]: url is required", i)
		}
		if seen[s.Name] {
			return fmt.Errorf("descriptor: duplicate source name %q — names must be unique, they qualify install commands", s.Name)
		}
		seen[s.Name] = true
	}
	return nil
}
