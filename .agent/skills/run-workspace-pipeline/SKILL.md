---
name: run-workspace-pipeline
description: 'Use to run the full agentic pipeline for a ticket from a workspace root — when the bundle is installed at a multi-repo workspace rather than inside a single repo.'
argument-hint: '[TICKET-ID]'
---

# Run Workspace Pipeline (Multi-Repo Orchestrator)

Drive one ticket across a workspace of member repos. This orchestrator owns only what has
no home in a single repo — ticket ingest, the split, work order, and PR cross-links. It
contains no stage logic: each repo is delivered by the existing `run-pipeline`.

## Preflight (fail loud — never guess)

1. The `AGENTS.md` at the current root has a `## Workspace members` section. If not,
   STOP: "Not a workspace root — run `run-pipeline` inside the repo instead." Never
   improvise a workspace from sibling directories or write a members list yourself.
2. Every member path exists, is a git repo, and has a clean working tree.
3. Every dependency edge between members has a linkage rule.

## Stages

Run **every** stage below as a fresh **dispatched subagent** (Task/Agent tool) — the
workspace-level `gather-context` and `decompose-ticket`, and each repo's `run-pipeline`.
Never run a stage inline in the workspace root: keep only each stage's status line and its
artifact in the root, so ticket ingest, the split, and full seven-stage per-repo runs never
pile into one context.

1. `gather-context` → `<workspace>/.agent/work/<TICKET>/context-bundle.md` (run once, at
   workspace level).
2. `decompose-ticket` → `repo-split.md` + one `context-bundle.<repo>.md` per touched
   repo. Verdict `BLOCKED-UNMAPPED-SCOPE` stops the run.
3. **Per touched repo, in the split's work order, one at a time — never in parallel:**
   1. **Reconcile contracts (dependent side).** For each already-built dependency A of this
      repo (A precedes it in the work order): read
      `<workspace>/.agent/work/<TICKET>/contract-A.md` (if the work order says A is built but
      the file is absent, STOP: `BLOCKED-MISSING-CONTRACT`) and mirror it into this repo's
      `.agent/work/<TICKET>/` — whatever the expectation says: a dependent that proceeds is
      planned against what A actually built, never against ticket prose alone. Then, if this
      repo's expectation for the A edge is `EXPECTATION-UNSPECIFIED`, there is no named
      surface to drift-check — refresh this repo's `context-bundle.<repo>.md` from the built
      contract, record that the edge relies on the mirrored contract (checked by the
      dependent's plan-stage Surfaces consumed rule), and proceed. Otherwise diff the built
      contract against this repo's expectation in `repo-split.md`:
      The classifier is an observable predicate, not a judgment call: **contradicts ⇔
      something the expectation NAMES — a name, signature/shape, path, or value — is absent
      or different in the built contract; everything else is detail.** Polarity: new exports,
      extra fields, added endpoints, or a new canonical path WHILE the named one still works
      are detail, however large they loom; a changed calling convention or return shape
      (e.g. sync → async), a removed named form, or a rename without the old name kept
      working is a contradiction, however much it reads as "built more than promised".
      - *adds detail* → note the refresh in `repo-split.md`, continue;
      - *contradicts* → STOP, verdict `BLOCKED-CONTRACT-DRIFT`, report built vs expected
        verbatim, side by side. Do NOT dispatch this repo against the stale expectation.
   2. Seed `<repo>/.agent/work/<TICKET>/`: copy `context-bundle.<repo>.md` in as
      `context-bundle.md`, mirror `repo-split.md`, copy the repo's assigned `assets/`.
   3. Deliver the repo by **dispatching a fresh subagent** (Task/Agent tool) that runs
      `run-pipeline <TICKET>` in that repo — **never invoke `run-pipeline` inline in the
      workspace root's context.** Inline pulls that repo's entire seven-stage pipeline into
      the root, defeating the thin-orchestrator design and compounding context across every
      repo. Its resume rule sees the seeded bundle and skips ingest. Await the subagent; it
      returns the repo's draft-PR URL (or a blocker) before the next repo starts. **"One at
      a time" is a no-concurrency rule, not a no-subagent rule** — dispatch the repos
      sequentially (await each before dispatching the next), never as parallel calls.
   4. **Extract its built contract (dependency side).** If this repo is the A-side of any
      touched edge, dispatch a fresh subagent to read its branch and write
      `<workspace>/.agent/work/<TICKET>/contract-<repo>.md` — the surface it actually built:
      every element of the repo's declared public surface (workspace AGENTS.md → Workspace
      members → Public surface) that the branch added or changed, quoted precisely
      (signatures, routes, shapes), plus each surface named by an expectation on an edge into
      it, confirmed as built or reported absent. A blank/placeholder Public surface knob is an
      unfilled knob: STOP and report — never infer the boundary. Extraction is never skipped
      because expectations are `EXPECTATION-UNSPECIFIED` — the contract is what dependents
      are planned against. (Keeps the orchestrator thin; the extraction transcript stays in
      the subagent.)
   5. Record the PR: append the repo's draft-PR URL to the workspace `repo-split.md`
      (beside that repo in the work order), so later repos' seeded mirrors carry every
      URL known so far; the cross-link pass completes the rest.
4. **Integration gate (whole-graph).** Once every touched repo has its draft PR, before
   cross-linking. Per-repo gates passing is NOT sufficient — it does not prove the assembled
   repos compose. Resolve the `Cross-repo e2e gate` knob (workspace AGENTS.md → Tooling / MCP):
   - **Configured** → dispatch a fresh subagent to link the member branches per the linkage
     rules, run the declared cross-repo e2e, and write
     `<workspace>/.agent/work/<TICKET>/integration-report.md` (`mode: e2e`). Failure → STOP
     and report. A missing/broken e2e executable is `BLOCKED-BY-ENV` (environment gap, not a
     code failure), not a silent pass.
   - **Not configured** → record `NO-INTEGRATION-GATE` and run a **graph-wide
     contract-satisfaction check**: for every touched edge B→A, assert A's `contract-A.md`
     satisfies (i) B's expectation in `repo-split.md` and (ii) every surface B's
     `implementation-plan.md` → `## Surfaces consumed` names on A. Any unmet expectation or
     consumed-but-absent surface → STOP, `BLOCKED-CONTRACT-DRIFT`. An edge with neither a
     stated expectation nor a Surfaces consumed entry cannot be statically checked — list it
     in `integration-report.md` as unverified; it neither passes nor fails the gate. Write
     `integration-report.md` (`mode: contract-static`).
   - Never skip silently: the report states which mode ran.
5. **Cross-link pass:** once every repo has its draft PR and the integration gate has run,
   edit each PR body (forge CLI, e.g. `gh pr edit`) so all of them list the full PR set in
   merge order, and surface the integration verdict.

## Rules

- **Run straight through the repos — no stopping between them.** Work the repos in order
  then the cross-link pass as one continuous run: the moment a repo's subagent returns
  without a blocker, seed and dispatch the next in the same turn. A repo's draft PR is a
  handoff back to you, not a checkpoint for the human — do not pause to ask "repo N's PR is
  up, proceed?". You stop only for a blocker, a plan-contradiction escalation, or the
  finished cross-linked PR set.
- Work order is the split's order — dependencies before dependents, however trivial a
  dependency looks. "The lib change is trivial, start the big repo first" is the failure
  mode, not a shortcut: the dependent's gates need the dependency's branch to exist.
- Seed before dispatch. A repo's `run-pipeline` must find its scoped bundle already in
  place; dispatching without seeding makes it re-ingest the FULL ticket — wrong scope,
  duplicated work in every repo.
- **Reconcile before you dispatch a dependent; extract after a dependency finishes.** A
  dependent repo is planned against `decompose`-time expectations — stale by the time its
  dependency is built. Never dispatch it without mirroring the dependency's built
  `contract-<repo>.md` into its work dir and refreshing its bundle — an unspecified
  expectation skips the drift-diff, never the mirror. A contradiction with a stated
  expectation STOPS (`BLOCKED-CONTRACT-DRIFT`) — never silently absorb a changed surface.
- If a later repo surfaces a finding that requires changing an earlier (finished) repo —
  including its plan stage STOPping `BLOCKED-CONTRACT-GAP` on a surface the dependency never
  built — STOP and report — plan-contradiction escalation. Do not silently reopen a finished
  repo.
- If any stage or repo-run reports a blocker, STOP the workspace run and report.
- **Every STOP is resumable.** Never delete artifacts on a STOP — they are the re-entry
  ledger. The per-verdict recovery table (what the human fixes, which artifacts to
  invalidate, where the run re-enters) is `README.md` → Resuming after a STOP.

## Output

One draft PR per touched repo, cross-linked with merge order. Report the PR URLs in merge
order. All artifacts stay local and uncommitted (workspace root and member repos alike):
per-repo trails under each `<repo>/.agent/work/<TICKET>/`, plus the workspace-level
`contract-<repo>.md` per dependency repo and `integration-report.md` recording the gate mode
and verdict.
