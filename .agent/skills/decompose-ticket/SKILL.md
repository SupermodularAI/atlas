---
name: decompose-ticket
description: "Use as the workspace stage after gather-context — when a run starts at a workspace root and the ticket's scope must be mapped onto member repos before any per-repo work."
argument-hint: '[TICKET-ID]'
---

# Decompose Ticket (Workspace Split)

Map the ticket's requirements onto the workspace's member repos and fix the order the
repos are worked in. Run as an isolated subagent — the only things crossing to the next
stage are the artifact files.

## Input

- `<workspace>/.agent/work/<TICKET>/context-bundle.md` from `gather-context`.
- The workspace `AGENTS.md` (members, dependency edges, linkage rules).

## Preflight (fail loud — never guess)

1. Read the context bundle. If it is absent, or flags a blocker that stops all work, STOP
   and report.
2. Read the workspace `AGENTS.md`. If it has no `## Workspace members` section, STOP:
   this is not a workspace root.

## Procedure

1. Map every requirement and acceptance criterion to exactly the member repos it lands
   in. Quote each verbatim — never paraphrase.
2. Restrict the workspace dependency edges to the touched repos and derive the work
   order: dependencies before dependents.
3. For each touched dependent repo, copy the linkage steps from its edge's linkage rule.
4. For each touched dependency edge B→A, capture B's **contract expectation** — the exact
   surface B consumes from A (function/type signatures, endpoints, schema fields), with each
   signature/shape quoted verbatim from the ticket wherever it states them (a surface is
   usually specified on A's side and consumed by B — capture it in full, never reduce it to a
   bare name). Never paraphrase and never invent a surface the ticket does not state; if the
   ticket names no concrete surface for the edge, record `EXPECTATION-UNSPECIFIED` for that edge.
5. If the ticket carries `assets/`, assign each asset to the repo(s) whose scope needs it.
6. Write one scoped context bundle per touched repo (`context-bundle.<repo>.md` — same
   sections as a normal bundle, holding only that repo's scope).

## Fail-loud rules

- A requirement that maps to no member repo is a GAP: set verdict
  `BLOCKED-UNMAPPED-SCOPE`, STOP, and report the requirement verbatim beside the member
  list. **Never stretch a repo's scope to absorb it, drop it, or defer it to keep the
  split moving** — a forced fit lands the work in the wrong repo, which is the failure
  this stage exists to prevent.
- A dependency cycle among the touched repos: STOP and report the cycle.
- The split resolving to a single repo is NOT a failure: set verdict `SINGLE-REPO` and
  continue with that one repo.

## Output artifacts (in `<workspace>/.agent/work/<TICKET>/`)

- `repo-split.md` — sections: Per-repo scope (requirements + acceptance criteria,
  verbatim) · Work order (with the edges that force it) · Linkage steps per dependent
  repo · **Contract expectations** (per touched edge B→A: the surface B consumes from A,
  quoted verbatim, or `EXPECTATION-UNSPECIFIED`) · Asset assignment (if any) · Verdict
  (`SPLIT-OK` / `SINGLE-REPO` / `BLOCKED-UNMAPPED-SCOPE`).
- `context-bundle.<repo>.md` — one per touched repo.
