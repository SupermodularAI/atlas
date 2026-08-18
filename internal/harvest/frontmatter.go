// Package harvest reads primitive metadata out of a cloned tree.
//
// Harvesting is not classifying: Atlas reads name and description from files
// and invents nothing. Values are returned verbatim — escaping belongs to the
// renderer.
package harvest

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

var fence = []byte("---")

// frontmatter mirrors the two fields Atlas reads from a primitive's YAML
// frontmatter block.
type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// ParseFrontmatter extracts name and description from a leading YAML
// frontmatter block. Missing or unterminated frontmatter is an error: a
// primitive Atlas cannot name is a primitive it must not silently list.
//
// Values are returned verbatim, including any markup — escaping is the
// renderer's job, not the parser's.
func ParseFrontmatter(content []byte) (string, string, error) {
	body := bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	body = bytes.TrimLeft(body, " \t\r\n")
	if !bytes.HasPrefix(body, fence) {
		return "", "", fmt.Errorf("no frontmatter block")
	}
	rest := body[len(fence):]
	if len(rest) > 0 && rest[0] != '\n' && rest[0] != '\r' {
		return "", "", fmt.Errorf("no frontmatter block")
	}
	end := bytes.Index(rest, append([]byte("\n"), fence...))
	if end < 0 {
		return "", "", fmt.Errorf("unterminated frontmatter block")
	}
	var fm frontmatter
	if err := yaml.Unmarshal(rest[:end], &fm); err != nil {
		return "", "", fmt.Errorf("parse frontmatter: %w", err)
	}
	return fm.Name, fm.Description, nil
}
