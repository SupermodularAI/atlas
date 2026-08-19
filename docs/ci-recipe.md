# Publishing an atlas from CI

Atlas produces two static files — `index.html` and `atlas.json`, ~7KB total, **zero external
requests** — so any static host can serve them with no build step at deploy time. `index.html` at
the root of an artifact directory is exactly what GitLab Pages and GitHub Pages expect.

This document is host-agnostic guidance plus one worked example. Atlas itself contains no host
knowledge (enforced by `internal/guard`), so nothing here is baked into the tool.

## The split that usually makes sense

| Piece | Where | Why |
|---|---|---|
| Atlas source | wherever you like, public | The tool is generic; it holds nothing of yours |
| Marketplace | wherever it already is | Unchanged |
| Descriptor | **your** private repo | It names your namespace — see design §3 |
| Published page | a members-only static host | It lists your private inventory |

The CI job belongs in the repo holding the **descriptor**, not in Atlas. It fetches the Atlas
binary, runs it, and publishes the output directory.

Source and page can live on different providers. Atlas reads a descriptor and whatever `git` can
reach; it neither knows nor cares where it was downloaded from.

## Authentication: read this before writing a pipeline

**`url.insteadOf` rewrites do not work with Atlas.** This is deliberate and worth understanding,
because it rules out the most commonly-copied CI recipe.

`internal/gitc` runs every clone with:

```go
cmd.Env = append(os.Environ(),
    "GIT_TERMINAL_PROMPT=0",        // never hang waiting for credentials
    "GIT_CONFIG_GLOBAL=/dev/null",  // a developer's personal config cannot
    "GIT_CONFIG_SYSTEM=/dev/null",  // change what Atlas reads
)
```

Neutralising global and system config is a correctness property: an atlas must not silently depend
on whose machine generated it. The cost is that config-based auth — `url.<base>.insteadOf`,
`credential.helper` set via `git config --global` — is invisible to Atlas.

What **does** work, because the parent environment passes through untouched:

1. **`.netrc` in `$HOME`** — git consults it regardless of config files. Cleanest for HTTPS.
2. **`GIT_ASKPASS`** pointing at a script that echoes a token.
3. **A token embedded in the descriptor's URLs** — works, but puts a secret in a file, so prefer 1
   or 2.
4. **SSH** via an injected key and `GIT_SSH_COMMAND`, if the manifest's `sourceBase` is SSH.

Which you need is decided by the **manifest's `sourceBase`**, not by the descriptor. Package URLs
resolve as `sourceBase + "/" + source`, and Atlas will not substitute a different scheme — doing so
would mean inventing provenance. So if `sourceBase` is `https://`, CI needs HTTPS credentials even
if the descriptor's marketplace URL is SSH.

## Worked example: GitLab CI + GitLab Pages

Place in the repo holding your descriptor. Requires a token with read access to the package repos —
`CI_JOB_TOKEN` suffices when they are in the same group.

```yaml
stages: [build]

pages:
  stage: build
  image: golang:1.26
  before_script:
    # HTTPS auth via .netrc, NOT url.insteadOf — see "Authentication" above.
    - echo "machine gitlab.com login gitlab-ci-token password ${CI_JOB_TOKEN}" > ~/.netrc
    - chmod 600 ~/.netrc
    # Pin the tool version. A moved tag must not change what generated the page.
    - go install github.com/<org>/atlas/cmd/atlas@v0.1.0
  script:
    - atlas --descriptor supermodular.yml --out public
  artifacts:
    paths: [public]
  rules:
    - if: $CI_PIPELINE_SOURCE == "schedule"
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
```

`public/` is GitLab Pages' expected artifact directory.

### Set Pages access control before the first run

GitLab Pages visibility is a **per-project setting independent of repo visibility** — a private
repo can publish a world-readable page. If the atlas lists anything non-public, set
Settings → General → Visibility → Pages to **"Only project members"** *before* the pipeline runs,
not after.

### Do not add `--strict` to a scheduled job

`--strict` exits non-zero on any degradation *or warning*, which is right for a gate and wrong for a
publisher: one unreadable package would mean no page at all, rather than a page that says a package
was unreadable. Degradation is information the page is designed to carry.

Use `--strict` in a separate validating job if you want CI to fail on it — then you get both a
published page and a red pipeline.

## Rebuild triggers: the non-obvious part

An atlas depends on the marketplace **and every package repo it names**. A pipeline in the
marketplace repo alone will miss a skill added to a package.

| Trigger | Catches | Misses |
|---|---|---|
| Push to the descriptor repo | descriptor edits | everything else |
| Push to the marketplace repo | packages added/removed, version bumps | changes *inside* a package |
| Scheduled (nightly) | everything, within a day | nothing, eventually |
| Multi-project trigger from each package repo | everything, immediately | nothing — but N pipelines to maintain |

**A scheduled rebuild plus a push trigger is usually the right answer.** A catalog is not
latency-sensitive, and the scheduled run is the one that catches a change nobody thought to wire up.

## Verifying a published atlas

The page states what it asserts, but two things are worth checking mechanically after a first
publish:

1. **Nothing excluded leaked.** Grep the published `atlas.json` and `index.html` for names belonging
   to excluded packages. This is the check the whole disclosure model reduces to.
2. **`warnings[]` is empty, or you understand every entry.** An `unused-exclude` warning means a
   pattern withheld nothing — so a package you intended to exclude was published. That mechanism
   failed silently four times during Atlas's own development, which is why the field exists.
