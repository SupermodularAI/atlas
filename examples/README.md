# Example

A self-contained fixture marketplace. Needs only `git` and the `atlas` binary —
no network, no access to any private repo.

```bash
make build
./examples/run-example.sh
open dist/example/index.html
```

It demonstrates three states on one page:

- **`pkg-demo`** — harvested, showing one skill (with a description read from
  frontmatter) and one hook (filename only, since hooks carry no frontmatter).
- **`pkg-withheld`** — listed by the marketplace, excluded by the descriptor.
  Its card renders with the manifest's name and description and its interior
  withheld, which is what an ACL-gated or confidential package looks like.
- The **claim boundary** paragraph, stating what the atlas does and does not
  assert.

The descriptor also excludes `pkg-nonexistent`, a pattern that matches nothing.
That's deliberate: it's the easy way to produce a `warnings[]` entry
(`unused-exclude`), so the page also demonstrates that rendering path. The
script does not pass `--strict` — a real `--strict` run would exit non-zero
here, since a recorded warning counts as degradation.
