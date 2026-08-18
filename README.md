# Atlas

Renders a company's published AI primitives into a browsable static site.

Atlas is a **reader**. It never classifies, builds, or publishes: it resolves published
APM marketplaces (or plain repos), harvests primitive metadata, and emits `atlas.json`
plus a self-contained `index.html`.

## What Atlas asserts

That these primitives were published at these sources, at these resolved commit SHAs,
read at this timestamp, by a principal with this much access.

**Atlas does not assert** that anything was approved, reviewed, unaltered, or authorised
to run. Approval state belongs to a governance control plane, not to a catalog renderer.

## Usage

```bash
atlas --descriptor company.yml --out ./site
```

A descriptor lists a company's sources — a company can have more than one marketplace:

```yaml
company: example-co
sources:
  - kind: marketplace          # a published APM marketplace
    name: example
    url: https://git.example.test/example-co/marketplace
    exclude:                   # never harvested; rendered as withheld
      - pkg-confidential

  - kind: repo                 # any repo with a .claude/ tree
    name: some-service
    url: https://git.example.test/example-co/some-service
    acknowledgeUnclassified: true
```

Descriptors belong in the company's own repo, not here.

## Documentation

- `docs/design.md` — the full design and the reasoning behind it
- `examples/` — a runnable fixture needing no access to anything private

## License

MIT
