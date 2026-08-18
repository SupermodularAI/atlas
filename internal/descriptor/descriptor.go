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

// KindMarketplace is a source backed by a package marketplace; excludes are
// exact package-name matches.
const KindMarketplace = "marketplace"

// KindRepo is a source backed by a git repository; excludes are path globs.
const KindRepo = "repo"

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

// matchGlob matches pattern against name or any ancestor directory of name,
// so a pattern naming a directory (with or without a trailing "/**")
// excludes everything beneath it — "skills/finance-*" and
// "skills/finance-*/**" both withhold "skills/finance-ops/a/b/SKILL.md". A
// trailing "/**" is accepted but redundant: the ancestor walk already
// crosses "/", which path.Match on its own never does. A bare or
// mid-pattern "**" is not supported — validate() rejects those forms at
// load time, so matchGlob never has to interpret one.
//
// matchGlob fails closed: validate() guarantees every pattern reaching here
// compiles, so a path.Match error at this point is an internal invariant
// violation, not user input. Reporting a match (excluded = true) rather than
// discarding the error is deliberate — withholding a primitive that should
// have been shown is a visible, correctable error; publishing one that
// should have been withheld is not.
func matchGlob(pattern, name string) bool {
	prefix := strings.TrimSuffix(pattern, "/**")
	for p := name; p != "." && p != "/" && p != ""; p = path.Dir(p) {
		ok, err := path.Match(prefix, p)
		if err != nil {
			return true
		}
		if ok {
			return true
		}
	}
	return false
}

// checkExcludePattern reports whether pat is a well-formed exclude glob for
// a repo-kind source: it must compile as a path.Match pattern (checked on
// the string matchGlob actually passes to path.Match — pat with any
// trailing "/**" trimmed, since that suffix is accepted but not itself
// matched), and it must not contain a non-trailing "**", which path.Match
// would silently treat as an ordinary single-segment "*" rather than the
// recursive match it looks like.
func checkExcludePattern(pat string) error {
	matchable := strings.TrimSuffix(pat, "/**")
	if strings.Contains(matchable, "**") {
		return fmt.Errorf("%q: \"**\" is only supported as a trailing \"/**\" suffix", pat)
	}
	// Self-match: matching a pattern against itself as the name forces
	// path.Match to scan the whole pattern for well-formedness, even past a
	// literal segment that would otherwise short-circuit the scan on a
	// mismatch before reaching a malformed character class further along.
	if _, err := path.Match(matchable, matchable); err != nil {
		return fmt.Errorf("%q: %w", pat, err)
	}
	return nil
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

		// marketplace excludes are exact string matches, not globs — no
		// pattern to validate.
		if s.Kind == KindRepo {
			for _, pat := range s.Exclude {
				if err := checkExcludePattern(pat); err != nil {
					return fmt.Errorf("descriptor: sources[%d] (%s): %w", i, s.Name, err)
				}
			}
		}
	}
	return nil
}
