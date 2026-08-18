---
name: run-pipeline
description: 'Use to run the full agentic pipeline for a ticket end-to-end — spawn one subagent per stage, hand off via `.agent/work` artifacts, and stop at the draft PR for human review.'
argument-hint: '[TICKET-ID]'
---

# Run Pipeline (Orchestrator)

Drive a ticket from the tracker to a draft PR. Spawn a FRESH subagent per stage so no stage
pollutes another's context. The only thing crossing stages is the `.agent/work/<TICKET>/`
artifact.

## Preflight (fail loud — never guess)

Verify before dispatching stage 1 — an unattended run cannot pause mid-flight for a human to fix
its environment, so a check that fails here STOPS the run with the finding:

1. `AGENTS.md` exists at the repo root with its required knobs FILLED — at minimum the verify-gate
   command (Stack & commands) and the branch policy. Template placeholder text (`<e.g. …>`) is
   NOT filled: never run a placeholder or guess a substitute command.
2. The working tree is clean. `.agent/work/` files are not dirt — they are never committed
   (should be gitignored; see Artifacts stay out of git), and leftovers for THIS ticket are
   resume material (see Rules → Resume, don't restart).
3. The forge CLI is authenticated with push rights (read-only check, e.g. `gh auth status`) —
   discovering a 403 at stage 7 wastes the entire run.

## Stages

1. `gather-context` → `context-bundle.md`
2. `check-architecture` → `architecture-notes.md` (verdict `BLOCKED-DECISION-GAP` stops the run)
3. `plan-implementation` → `implementation-plan.md`
4. `implement-plan` → code + commits on `<type>/<TICKET>/<slug>` (`<type>` = ticket's commit type)
5. `verify-changes` → `validation-report.md` (loop 4 ↔ 5 until PASS)
6. `pr-review` → `pr-review-report.md` (isolated subagent; blocking findings loop back to 4)
7. `pr-describe-draft` → open the draft PR

## How to run each stage

Run every stage as a **dispatched subagent** (the Task/Agent tool) — **never** invoke a stage skill
inline in your own context. Inline invocation is the low-friction trap: it pulls the stage's ticket
content, diffs, and research into the orchestrator, defeating the whole design. For stage N:

1. Dispatch a fresh subagent with a prompt of the shape: *"Invoke the `<stage>` skill for
   `<TICKET>`. Read your inputs from `.agent/work/<TICKET>/`, write your output artifact there, and
   report back ONLY a status line — `PASS` or `BLOCKER: <reason>` — plus the artifact path. Do not
   return the artifact body."* Run it foreground (stages are sequential — each needs the previous
   artifact).
2. On return, read only that status line and confirm the artifact file exists. You do **not** need
   the artifact body in your context — the next stage's subagent reads it from disk; read it
   yourself only when a routing decision needs it (e.g. which `pr-review` finding to loop back on).
   Across the whole run your context holds only short status lines and file-existence checks.
3. `BLOCKER` (or a stopping verdict like `BLOCKED-DECISION-GAP`) → STOP and report. Otherwise
   proceed to stage N+1.

**Red flag — STOP:** you are about to call a stage skill (`gather-context`, `plan-implementation`,
…) directly in your own context. Dispatch it as a subagent instead.

## Artifacts stay out of git

`.agent/work/` is a **local working surface**: the stages' handoff files and the run's resume
ledger live on disk only and die with the branch. **Never commit, stage, or push anything under
`.agent/work/`** — there are no artifact commits at any stage boundary, and no commit in the run
may touch a path under it (the only commits are the implement stage's, scoped to source). The
host repo gitignores `.agent/work/` at install; if this one does not, report the gap in your
final summary — it is not a reason to commit the files.

## Rules

- **Run straight through — no stopping between stages.** This is ONE continuous execution. The
  moment a stage's subagent returns without a blocker, dispatch the next stage **in the same turn**.
  Do not end your turn, do not post a "Stage N done — shall I proceed?" summary, do not wait for the
  human to say "continue". Reaching a stage's `## Output artifact` is a handoff, not a finish line.
  You stop for exactly three things: a stage blocker, the optional plan gate (below, only if
  requested), and the final draft PR. **Red flag:** ending a turn anywhere between stages 1 and 7
  with the work un-blocked is the failure this rule exists to prevent.
- Default: NO human checkpoint before the PR. **Optional plan gate (off by default):** if the
  human requests it when starting the run, STOP after stage 3, present `implementation-plan.md`,
  and wait for approval before launching `implement-plan` (stage 4).
- **Loop until clean — bounded.** If `verify-changes` (5) FAILS, return to (4). If `pr-review` (6)
  has blocking findings, return to (4) → re-run (5) → re-run (6). Only proceed to
  `pr-describe-draft` (7) when (5) PASSES and (6) has no blocking findings. The **Attempt log** in
  `validation-report.md` is the loop's ledger, and YOU enforce the cap — each implement subagent
  is fresh and cannot count its predecessors: after **3 verify FAILs with the same failure
  signature**, or **2 `pr-review` loop-backs that do not shrink the blocking list**, STOP and
  report a suspected unsound approach. Do not dispatch attempt #4 — with no new information it is
  mechanically identical to attempt #1.
- If any stage reports a blocker (ingest auth failure, unresolved dependency, failing gate),
  STOP and report — never guess past it.
- **Resume, don't restart.** The `.agent/work/<TICKET>/` artifacts ARE the progress ledger. After
  any compaction or resume, trust the artifacts + `git log` over recollection: a stage whose
  artifact exists and is complete is DONE — never re-run it.
- **Plan-contradiction escalation.** If any stage surfaces a finding that contradicts what the
  `implementation-plan.md` text states about something the TICKET names (a name, signature,
  path, or value stated in the context bundle's requirement/AC text), STOP and report the
  finding beside the plan text — do not let a later stage silently resolve it. A plan statement
  contradicted only by repo reality is not a run-stopper: the implement stage repairs it and
  records it in the plan's `## Deviations` section (its Plan ↔ reality discipline). Everything
  the plan does not name is added detail and never stops the run.

## Output

A draft PR for the ticket, ready for human review; the run's artifact trail stays local under
`.agent/work/<TICKET>/`.
