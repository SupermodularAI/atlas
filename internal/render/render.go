// Package render turns an atlas into a self-contained HTML page.
//
// html/template escapes every interpolation by default, which is the injection
// guard for third-party frontmatter text — internal/harvest deliberately
// returns descriptions verbatim, including markup, so this is the only place
// that escaping happens. Never switch to text/template or build HTML by
// string concatenation: either would silently reopen the hole this package
// exists to close.
package render

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"

	"github.com/SupermodularAI/atlas/internal/model"
)

//go:embed page.gohtml
var files embed.FS

// Render produces the complete page for a. No external requests: all CSS is
// inline and there is no script or remote resource, so a generated atlas
// opens correctly straight from disk.
func Render(a *model.Atlas) ([]byte, error) {
	t, err := template.ParseFS(files, "page.gohtml")
	if err != nil {
		return nil, fmt.Errorf("render: parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, a); err != nil {
		return nil, fmt.Errorf("render: execute template: %w", err)
	}
	return buf.Bytes(), nil
}
