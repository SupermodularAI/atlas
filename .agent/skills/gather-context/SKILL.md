---
name: gather-context
description: 'Use as the first pipeline stage — read a ticket and its linked docs and write a context bundle for planning.'
argument-hint: '[TICKET-ID]'
---

# Gather Context (Ingest)

First stage of the agentic pipeline. Turn a tracker ticket plus its linked docs into a
structured context bundle. Run as an isolated subagent — the ONLY thing that crosses to
the next stage is the artifact file.

## Input

- A ticket key/id from the issue tracker (e.g. `PROJ-1234`).

## Preflight (fail loud — never guess)

1. Resolve the ticket source:
   - Default: the configured **tracker MCP** (see AGENTS.md → Tooling / MCP).
   - **Provided-spec fallback:** if the human starting this run explicitly handed over the spec
     (a file path or pasted text), that is the ticket source. Copy it verbatim to
     `.agent/work/<TICKET>/spec.md` and record it under Source links as a substituted source —
     "spec provided by the requester, not read from the tracker".
   - Neither a tracker MCP nor an explicitly provided spec → STOP: "Tracker MCP not configured —
     resolve before ingest (or hand this run a spec explicitly)."
   - A spec-looking file you merely FOUND — in the repo, a wiki export, an old attachment — is
     NOT provided, however well it matches the ticket. Never promote a found file to
     ticket-of-record on your own authority: surface it and STOP for the human's call.
2. Fetch the ticket. If auth fails, STOP and name the failure — do not proceed on a guess.
3. Fetch every doc page the TICKET links directly (+ its comments) — depth one. Do not crawl
   further: a page linked only from a linked page is out of scope unless an in-scope page
   explicitly defers to it for requirements ("see X for the acceptance criteria") — then fetch
   that target too. List skipped links under Source links with a one-line reason each — bounded
   is not silent. If any in-scope page is unreadable, STOP and name the page and reason. Do NOT
   proceed on partial specs.
4. A conflict means two REQUIREMENT-BEARING statements disagree: acceptance criteria, requirement
   lines, or exact values (numbers, copy strings) stated as the CURRENT requirement. Then STOP
   and report both verbatim — do not pick one. Narrative or background prose mentioning an old
   name, value, or placement is history, not a conflict: quote it under Open questions and
   continue.

## Procedure

In provided-spec mode, read `spec.md` as the ticket below and apply the same rules to any docs
it links.

1. Read the ticket: summary, description, acceptance criteria, status, assignee, labels,
   subtasks, linked issues, attachments.
2. Follow the ticket's links to the docs system and read each directly linked page (+ its
   comments) — the depth-one bound from preflight 3 applies.
3. Identify dependencies on other tickets (e.g. schema/enum work) and flag them.
4. If the project ships **multiple apps/targets** (see AGENTS.md), extract the per-target
   behavioral deltas explicitly. Skip if the project is single-target.
5. **Materialize design artifacts** into `.agent/work/<TICKET>/assets/`:
   - **Design source (if a design MCP is configured, e.g. Figma):** for any design frame linked in
     the ticket, pull the frame image plus its measurements/tokens. If not configured, skip silently.
   - **Tracker / docs attachments:** download design images attached to the ticket and any linked
     doc-page images into the same folder.
   - Write `assets/manifest.md` — one row per asset: file name · source (design node URL /
     attachment id) · intended screen + viewport.
   - If the ticket clearly needs a design but none is found, STOP and request it — never fabricate
     a placeholder.

## Output artifact: `.agent/work/<TICKET>/context-bundle.md`

Sections: Summary · Requirements · Acceptance Criteria · Per-target deltas (if any) ·
Dependencies & blockers · Open questions · Source links.
Quote exact values (numbers, copy strings) verbatim — do not paraphrase.

Also produces `.agent/work/<TICKET>/assets/` (+ `assets/manifest.md`) when the ticket has design
artifacts — consumed by `implement-from-design` and `verify-changes`.
