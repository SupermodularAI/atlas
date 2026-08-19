// Command atlas renders a company's published primitives into a static site.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/SupermodularAI/atlas/internal/build"
	"github.com/SupermodularAI/atlas/internal/descriptor"
	"github.com/SupermodularAI/atlas/internal/render"
)

func main() {
	var (
		descPath string
		outDir   string
		strict   bool
	)
	root := &cobra.Command{
		Use:   "atlas",
		Short: "Render a company's published AI primitives into a static site",
		Long: "Atlas reads a company descriptor, harvests primitive metadata from published\n" +
			"marketplaces and plain repos, and writes atlas.json plus a self-contained\n" +
			"index.html.\n\n" +
			"Atlas is a reader: it never classifies, builds, or publishes.",
		SilenceUsage: true,
		// SilenceErrors: cobra's own Execute() error path would otherwise print
		// "Error: <msg>" itself, and main()'s explicit Fprintln below would print
		// the same message again — every abort path doubled the error line an
		// operator has to read. Print it exactly once, with the atlas: prefix
		// that identifies the tool in a CI log.
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(descPath, outDir, strict)
		},
	}
	root.Flags().StringVar(&descPath, "descriptor", "", "path to the company descriptor (required)")
	root.Flags().StringVar(&outDir, "out", "", "output directory (required)")
	root.Flags().BoolVar(&strict, "strict", false, "exit non-zero if any source or package degraded, or any warning was recorded")
	_ = root.MarkFlagRequired("descriptor")
	_ = root.MarkFlagRequired("out")

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "atlas:", err)
		os.Exit(1)
	}
}

// run loads the descriptor, builds the atlas, and writes atlas.json and
// index.html to outDir. A configuration error (fail-closed repo source,
// symlink escape, unresolved ref) aborts before anything is written — a
// partial atlas on disk would look complete and so is worse than none.
//
// Degradation (an unavailable source, a restricted package, or a recorded
// warning) never aborts the run on its own: it is always reported on stderr,
// and only turns the run non-zero when strict is true.
func run(descPath, outDir string, strict bool) error {
	d, err := descriptor.Load(descPath)
	if err != nil {
		return err
	}

	a, err := build.Build(build.Options{
		Descriptor: d,
		Now:        func() string { return time.Now().UTC().Format(time.RFC3339) },
	})
	if err != nil {
		return err
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create out dir: %w", err)
	}

	// Atlas's output is regenerable by construction, so re-running into a
	// non-empty outDir silently overwrites atlas.json/index.html rather than
	// refusing — but an operator re-running the tool should be told their
	// previous artifacts were replaced, not left to notice by diffing them
	// themselves. Check for pre-existence before writing, since the write
	// itself destroys the evidence.
	var overwritten []string
	for _, name := range [...]string{"atlas.json", "index.html"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err == nil {
			overwritten = append(overwritten, name)
		}
	}

	js, err := a.MarshalJSONIndent()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "atlas.json"), js, 0o644); err != nil {
		return fmt.Errorf("write atlas.json: %w", err)
	}
	html, err := render.Render(a)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "index.html"), html, 0o644); err != nil {
		return fmt.Errorf("write index.html: %w", err)
	}
	if len(overwritten) > 0 {
		fmt.Fprintf(os.Stderr, "overwritten: %s (already present in %s)\n",
			strings.Join(overwritten, ", "), outDir)
	}

	// Always report bounded coverage: a run that quietly omitted sources or
	// packages while exiting 0 would read as "covered everything". This is
	// the only place a human learns coverage was bounded, so it is printed
	// unconditionally, to stderr, so stdout stays clean for scripting.
	fmt.Fprintf(os.Stderr, "%d sources: %d read, %d unavailable · %d packages: %d harvested, %d restricted, %d withheld\n",
		len(a.Sources), a.Summary.Sources["read"], a.Summary.Sources["unavailable"],
		len(a.Packages), a.Summary.Packages["harvested"],
		a.Summary.Packages["restricted"], a.Summary.Packages["excluded"])
	if len(a.Collisions) > 0 {
		fmt.Fprintf(os.Stderr, "%d name collision(s) reported on the page\n", len(a.Collisions))
	}
	if n := len(a.Warnings); n > 0 {
		fmt.Fprintf(os.Stderr, "%d warning(s): a control may have had no effect — see atlas.json warnings[]\n", n)
	}

	if strict {
		unavailable := a.Summary.Sources["unavailable"]
		restricted := a.Summary.Packages["restricted"]
		warned := len(a.Warnings)
		if unavailable+restricted+warned > 0 {
			// Named per condition, never collapsed into one integer: §7 requires
			// the two degradation levels stay distinguishable, and a warning is a
			// third, different thing (an ineffective control, not missing
			// coverage). An operator fixing this needs to know which counter is
			// non-zero, not just that "something" was non-zero.
			return fmt.Errorf("--strict: %d unavailable source(s), %d restricted package(s), %d warning(s)",
				unavailable, restricted, warned)
		}
	}
	return nil
}
