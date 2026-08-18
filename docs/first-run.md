# First run against a real marketplace

**Date:** 2026-08-18
**Binary:** built from `feat/ATLAS-11/guard-example` @ `c8c8136`
**Marketplace:** a private GitLab group, 8 published packages

The descriptor is deliberately **not** in this repo — it names a private namespace, and §3 places
descriptors in the company's own repo rather than in the public tool. Neither the generated
`atlas.json` nor `index.html` is committed either: the JSON is a full primitive inventory of a
private marketplace.

## Result

```
1 sources: 1 read, 0 unavailable · 8 packages: 0 harvested, 6 restricted, 2 withheld
exit=0
```

| State | Count | Meaning |
|---|---|---|
| harvested | 0 | — |
| restricted | 6 | Atlas could not read them (see transport, below) |
| withheld | 2 | excluded by descriptor |

No warnings, no collisions.

## The disclosure guarantee held

The check that matters most: grep both artifacts for content belonging to the two excluded
packages — skill names and distinctive description terms. **Zero hits in `atlas.json`, zero in
`index.html`.**

Both excluded packages still render as withheld cards carrying their manifest-supplied name and
description, with `primitives: null` and `reason: "excluded by descriptor"`. Absent packages would
have been silent truncation, which §7 forbids.

**`warnings[]` was empty, and that is the affirmative signal.** Had either exclude pattern failed to
match a real package name, Atlas would have emitted `unused-exclude` — and harvested the package.
An empty warnings array means both patterns matched. The mechanism that silently withheld nothing
four times in this project's history now proves it withheld something.

Note the ordering that makes this robust: exclusion happens **before** any clone is attempted, so
the two withholdings are independent of the transport failure below. Had exclusion depended on a
successful clone, the transport problem would have masked whether it worked at all.

## A real limitation found: transport mismatch

The 6 restricted packages all failed identically:

```
access denied: could not read Username for 'https://gitlab.com': terminal prompts disabled
```

The cause is a mismatch Atlas cannot resolve. The **marketplace** URL in the descriptor is SSH,
which is the auth configured on this machine. But **package** URLs resolve as
`sourceBase + "/" + source`, and `sourceBase` is baked into the published manifest as an `https://`
URL. So the operator authenticates one way and the manifest dictates another.

Whose limitation this is, precisely:

- **Not a defect.** Atlas read `sourceBase` from the manifest instead of substituting a guess —
  exactly what §2's host-agnostic requirement demands. Substituting the marketplace's scheme would
  be Atlas inventing provenance.
- **Not fixable by the operator.** No descriptor setting overrides `sourceBase`; it comes from the
  published manifest.
- **The same root cause as the stale namespace** (§14). The manifest's `sourceBase` is stale in both
  path and scheme, because it is a hardcoded constant in the emitter that publishes it.

Two honest options for a future run, neither implemented: configure an HTTPS credential helper so
the operator's git can satisfy the manifest, or fix `sourceBase` in the emitter. The second is
better — it fixes the published artifact rather than working around it.

## §14's prediction confirmed

`resolvedFrom` recorded the **vacated** namespace for every package — the path the repos moved away
from on 2026-08-18 — rather than the group they now live in. That is the designed behaviour: Atlas
records what it actually resolved, so a stale `sourceBase` becomes visible in the artifact instead
of hiding behind GitLab's redirect.

An operator reading this atlas learns the manifest is stale. One reading a tool that silently
substituted the correct path would not.

## Design decisions this run validated

Four, each from a different task:

1. **`GIT_TERMINAL_PROMPT=0`** — the first attempt (HTTPS marketplace URL) failed in under a second
   instead of hanging on a credential prompt. Deliberate, for unattended runs.
2. **`failureReason` extraction** — the recorded reason names the cause
   (`could not read Username ... terminal prompts disabled`) rather than the `Cloning into
   '/tmp/atlas-clone-...'` boilerplate the original code returned. That fix was promoted from
   non-blocking precisely because this string lands in published output; here it is, in published
   output.
3. **Degradation is recorded, never fatal** (§7) — a completely unreadable source produced a valid
   artifact with `status: unavailable` and a reason, exit 0. Atlas neither crashed nor pretended the
   marketplace was empty.
4. **The two degradation levels stay distinct** — the rendered page says *"Atlas was not able to
   read this package"* for the six, and *"Atlas could have read this package but was told to
   withhold it"* for the two. Different claims about what Atlas knows, exactly as §7 requires.

## What is deliberately not recorded here

No content from the two excluded packages: no skill names, no descriptions beyond what the
marketplace manifest itself publishes about the package as a whole. The run's purpose was that
Atlas withheld them; a write-up quoting them would defeat it. Recorded: *that* they were withheld
and *why*. Not *what*.
