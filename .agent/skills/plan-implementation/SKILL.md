---
name: plan-implementation
description: 'Use as the planning pipeline stage — after check-architecture and before implement-plan — turn a context bundle into a file-by-file implementation plan, invoking a domain playbook when the ticket matches one.'
---

# Plan Implementation

Planning stage. Read the context bundle and produce a concrete, file-by-file plan.
Run as an isolated subagent. Honor the project conventions in AGENTS.md (keep changes minimal
and surgical — only what the ticket requires).

## Procedure

1. Read `.agent/work/<TICKET>/context-bundle.md` and, if present,
   `.agent/work/<TICKET>/architecture-notes.md`. Treat the notes' **Binding constraints for the
   plan** as non-negotiable — every task must respect them. If the notes read
   `BLOCKED-DECISION-GAP`, surface the gap at the top of the plan and STOP — do not plan around a
   decision that has not been made. If either file flags a blocker that blocks all work, STOP and
   report.
2. Explore the repo for the touched areas (grep / Explore subagent). Identify exact files.
3. If the ticket matches a known pattern, invoke the matching playbook and fold in its steps:
   - ticket carries design artifacts (`.agent/work/<TICKET>/assets/` non-empty) → `implement-from-design`
   - any other repo-specific playbook the project ships under `.agent/skills/`.
4. **Dependency contracts (workspace runs):** if the work dir holds mirrored dependency
   contracts (`contract-*.md`), they are ground truth for what each dependency actually built.
   The plan MUST carry a `## Surfaces consumed` section — per dependency, every surface the
   plan calls, quoted verbatim from its contract; plan against nothing a contract does not
   state. If a requirement needs a surface no contract provides, STOP: verdict
   `BLOCKED-CONTRACT-GAP`, the requirement and the missing surface reported side by side.
   Never absorb the gap to keep moving — not by descoping the requirement, emitting a
   "partial" plan, or inventing the surface; whether the ticket or the dependency changes is
   the orchestrator's escalation, not the planner's call.
5. Write the plan: per-file changeset; test plan that follows the project testing strategy
   (see AGENTS.md → Testing strategy) — honor its test-file naming/colocation rule; risks;
   the exact verify command(s).
   Each task MUST carry a **verifiable success criterion** — the exact check that proves it
   done — so the implement stage can loop independently (goal-driven execution).
6. **Self-review the plan against the context bundle before finishing:** (a) every acceptance
   criterion maps to a task — list any gaps; (b) scan for the plan failures below; (c) names and
   signatures used in late tasks match those defined in earlier tasks; (d) every file path and
   symbol the plan names was verified against the repo THIS session — a bundle's repo-layout
   claims are unverified input, not evidence (audits go stale); paths the plan creates are
   marked NEW; (e) if contracts are mirrored: the Surfaces consumed section exists and every
   entry appears verbatim in a contract. Fix inline.
7. Never touch the off-limits paths declared in AGENTS.md.

**Plan failures (never emit):** `TBD` / `TODO` / "implement later"; "handle edge cases" or "add
validation" with no concrete change; "write tests" without naming the test file + the cases; a
path that doesn't exist; a type or function referenced but defined in no task.

## Output artifact: `.agent/work/<TICKET>/implementation-plan.md`

TDD, bite-sized steps with exact paths and verification commands. No placeholders.

If the run was started with the optional plan gate enabled, this artifact is the checkpoint: the
orchestrator pauses here for human approval before `implement-plan`.
