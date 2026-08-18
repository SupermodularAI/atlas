---
name: implement-from-design
description: 'Use when a ticket carries design artifacts — implement frontend code from the materialized assets, resolving to theme tokens and confirming visual fidelity.'
---

# Implement From Design (Playbook)

Implement frontend code from the design artifacts materialized by `gather-context` into
`.agent/work/<TICKET>/assets/`. Pairs with the visual review in `verify-changes`'
behavioral-confirmation gate, which checks the result against the same assets.

## Prerequisite

`.agent/work/<TICKET>/assets/` exists and is non-empty (see `assets/manifest.md`). If the ticket
is visual but the folder is empty, STOP and request the design — never guess the layout.

## Steps

1. **Read the assets.** Open each image in `assets/` and read `manifest.md` for the intended
   screen + viewport. Note exact spacing, color, typography, sizing, and states.
2. **Map to the design system before writing code:**
   - Resolve every color / spacing / typography value to the project's **theme tokens** — never
     hardcode a hex/rgb/named value (AGENTS.md → Coding conventions → styling/theming).
   - If a design value has no theme-token equivalent (a color or spacing absent from the project's
     tokens), do not hardcode it and do not invent a token — STOP and report the value + the screen
     so the token gap is resolved deliberately.
   - Reuse an existing component before building a new one.
   - Place new code at the correct architectural layer for the project's structure.
3. **Implement** following TDD and the conventions in `implement-plan` (language strictness,
   naming, import order, accessibility rules — all per AGENTS.md).
4. **Confirm fidelity** via the visual review in `verify-changes`' behavioral-confirmation gate
   (browser-automation MCP, structured review against `assets/`). Loop on reported deviations
   until clean.

## Verify

The verify gate passes, and the `verify-changes` behavioral-confirmation gate reports no
outstanding visual deviations against `assets/` (or `BLOCKED-BY-ENV` if no browser-automation MCP
is available).
