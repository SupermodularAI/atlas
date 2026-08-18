#!/usr/bin/env bash
# Builds the committed fixture into throwaway git repos, then renders an atlas
# from them. Requires only git and the atlas binary — no network, no access to
# anything private.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

mkrepo() {
  local src="$1" dest="$2"
  mkdir -p "$dest"
  cp -R "$src/." "$dest/"
  git -C "$dest" init -q -b main
  git -C "$dest" add -A
  GIT_AUTHOR_NAME=example GIT_AUTHOR_EMAIL=example@example.test \
  GIT_COMMITTER_NAME=example GIT_COMMITTER_EMAIL=example@example.test \
    git -C "$dest" commit -q -m fixture --no-gpg-sign
}

mkrepo "$here/fixture/pkg-demo" "$work/pkg-demo"

mkdir -p "$work/marketplace"
cat > "$work/marketplace/apm.yml" <<YAML
name: example-marketplace
version: 1.0.0
description: A fixture marketplace for the Atlas example.
marketplace:
  owner:
    name: example-co
  sourceBase: file://$work
  packages:
    - name: pkg-demo
      description: "A demonstration package containing one skill and one hook."
      source: pkg-demo
    - name: pkg-withheld
      description: "Listed by the marketplace, withheld by the descriptor."
      source: pkg-demo
YAML
mkrepo "$work/marketplace" "$work/marketplace-repo"

cat > "$work/demo.yml" <<YAML
company: example-co
sources:
  - kind: marketplace
    name: example
    url: file://$work/marketplace-repo
    exclude:
      - pkg-withheld
      - pkg-nonexistent
YAML

"$root/atlas" --descriptor "$work/demo.yml" --out "$root/dist/example"
echo "Rendered → $root/dist/example/index.html"
