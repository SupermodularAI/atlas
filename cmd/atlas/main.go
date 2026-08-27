// Command atlas renders a company's published primitives into a static site.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/SupermodularAI/atlas/internal/gitc"
	"github.com/SupermodularAI/atlas/internal/harvest"
	"github.com/SupermodularAI/atlas/internal/resolve"

	"github.com/SupermodularAI/atlas/internal/build"
	"github.com/SupermodularAI/atlas/internal/descriptor"
	"github.com/SupermodularAI/atlas/internal/render"
)

// version is stamped at build time via -ldflags "-X main.version=...", and is
// "dev" for anything built without it.
//
// It exists so a published atlas can be traced to the tool that produced it. A
// consumer pins a release binary by SHA256 rather than by tag — a tag is a
// mutable pointer, a checksum is not — and this is how the pinned artifact
// identifies itself once it is running.
var version = "dev"

func main() {
	var (
		descPath     string
		outDir       string
		strict       bool
		manifestPath string
	)
	root := &cobra.Command{
		Use:   "atlas",
		Short: "Render a company's published AI primitives into a static site",
		Long: "Atlas reads a company descriptor, harvests primitive metadata from published\n" +
			"marketplaces and plain repos, and writes atlas.json plus a self-contained\n" +
			"index.html.\n\n" +
			"Atlas is a reader: it never classifies, builds, or publishes.",
		Version:      version,
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

	// `atlas check` is the authoring-side gate for the publishing-side reader.
	//
	// It lives in Atlas rather than as a script in each package repo so that the
	// gate and the consumer share one YAML parser. A checker built on a different
	// implementation can disagree at the margins, and a gate that passes what the
	// page then rejects is worse than no gate: it teaches confidence it has not
	// earned. It also means eight content-only repos need no new dependency.
	check := &cobra.Command{
		Use:   "check [dir]",
		Short: "Report frontmatter that would stop a primitive being listed, or list it wrongly",
		Long: "Walks a tree and reports every primitive whose frontmatter Atlas could not\n" +
			"read, or could read only partially.\n\n" +
			"Two failure modes, and the second is why this is not merely a parse check:\n" +
			"an unquoted value containing \": \" is INVALID YAML and the primitive is\n" +
			"omitted; an unquoted value containing \"#\" is VALID YAML, silently truncated\n" +
			"at the \"#\", and is listed wrongly with nothing reported anywhere.\n\n" +
			"Exits non-zero if anything is found, so it can gate a merge request.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if manifestPath != "" {
				return checkManifest(manifestPath)
			}
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			defects, checked, err := harvest.Lint(dir)
			if err != nil {
				return err
			}
			fmt.Printf("checked %d primitive frontmatter block(s) under %s\n", checked, dir)
			if len(defects) == 0 {
				fmt.Println("frontmatter OK")
				return nil
			}
			for _, d := range defects {
				fmt.Fprintf(os.Stderr, "  %s\n      %s\n", d.Path, d.Reason)
			}
			return fmt.Errorf("%d frontmatter problem(s) — quote the value in double quotes", len(defects))
		},
	}
	check.Flags().StringVar(&manifestPath, "manifest", "",
		"instead of linting a tree, verify every package version in this marketplace manifest resolves to a real tag")
	root.AddCommand(check)

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
// checkManifest verifies that every version pinned in a marketplace manifest
// resolves to a tag that actually exists upstream.
//
// This closes a failure mode that reports nothing at all. tagPattern turns each
// package's `version:` into the ref Atlas fetches, so those pins are the ONLY
// thing selecting content. Two ways that goes wrong silently:
//
//   - a package is re-tagged upstream but not bumped here. Atlas keeps reading
//     the OLD tag, successfully. Nobody sees the new content and nothing errors.
//   - a version is bumped here but never tagged upstream. Nothing complains
//     until something tries to fetch it.
//
// Neither is visible by reading either repo alone, which is why the check has to
// compare the two. It uses ls-remote, so it costs no clone.
func checkManifest(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	man, err := resolve.ParseManifest(data)
	if err != nil {
		return err
	}
	if man.TagPattern == "" {
		fmt.Printf("%s pins no tagPattern — versions do not select a ref, nothing to verify\n", path)
		return nil
	}

	var problems []string
	for _, pkg := range man.Packages {
		url, uerr := man.ResolveURL(pkg)
		if uerr != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", pkg.Name, uerr))
			continue
		}
		ref := man.ResolveRef(pkg)
		if ref == "" {
			fmt.Printf("  %-24s no version pinned — resolves to the default branch\n", pkg.Name)
			continue
		}
		ok, rerr := gitc.RefExists(url, "refs/tags/"+ref)
		switch {
		case rerr != nil:
			// An unreachable remote is not the same finding as a missing tag,
			// and must not be reported as one.
			problems = append(problems, fmt.Sprintf("%s: could not verify %s (%v)", pkg.Name, ref, rerr))
		case !ok:
			problems = append(problems, fmt.Sprintf(
				"%s: pinned %q but tag %s does not exist upstream", pkg.Name, pkg.Version, ref))
		default:
			fmt.Printf("  %-24s %s\n", pkg.Name, ref)
		}
	}

	fmt.Printf("checked %d package pin(s) in %s\n", len(man.Packages), path)
	if len(problems) == 0 {
		fmt.Println("every pinned version resolves to a real tag")
		return nil
	}
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, "  "+p)
	}
	return fmt.Errorf("%d package pin(s) do not resolve", len(problems))
}

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
