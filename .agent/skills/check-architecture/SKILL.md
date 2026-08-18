---
name: check-architecture
description: 'Use as the solution-design pipeline stage — after gather-context and before plan-implementation — when a ticket is ready to plan and the repo may carry architecture decisions the change must respect.'
argument-hint: '[TICKET-ID]'
---

# Check Architecture (Solution Design)

Solution-design stage. Turn the context bundle plus the repo's architecture decisions into the
binding constraints planning must honor. Run as an isolated subagent — the only thing crossing to
the next stage is the artifact file.

## Input

- `.agent/work/<TICKET>/context-bundle.md` from `gather-context`.

## Preflight (fail loud — never guess)

1. Read the context bundle. If it is absent, or flags a blocker that stops all work, STOP and report.
2. Resolve the ADR source from AGENTS.md → Architecture decisions. If it is marked **not
   configured**, write `architecture-notes.md` with verdict `NO-ADR-SOURCE` and return — do NOT
   fabricate constraints and do NOT block the pipeline.

## Procedure

1. Load the ADRs / reference architecture from the configured source.
2. Select only the decisions that bear on THIS ticket's requirements; ignore the rest.
3. Distill each into a one-line binding constraint the plan must honor.
4. Detect any architecture decision the ticket forces that no loaded ADR covers.

## Fail-loud rule

A decision the ticket forces with no covering ADR is a GAP. Set verdict `BLOCKED-DECISION-GAP`,
STOP, and report the decision needed beside the requirement that forces it. **Never invent an ADR,
pick a direction, or defer the decision to keep moving** — recording a guess as a binding
constraint is the exact failure this stage exists to prevent.

## Output artifact: `.agent/work/<TICKET>/architecture-notes.md`

Sections: Applicable decisions (id · constraint · source link) · Binding constraints for the plan ·
Gaps / unmade decisions · Verdict (`CONSTRAINTS-EXTRACTED` / `NO-ADR-SOURCE` /
`BLOCKED-DECISION-GAP`). Quote ADR ids and their constraints exactly — never paraphrase a decision
into something weaker.
