---
name: verify-changes
description: 'Use as the verification pipeline stage — after implement-plan — run the validate gates and write a validation report; loop back to implementation on failure.'
---

# Verify Changes

Verification stage. Run the quality gates and record the result. The exact commands come from
AGENTS.md → Stack & commands; this skill defines the process, not the tooling.

## Procedure

1. **Format check.** If the verify gate does not cover formatting but CI checks it separately
   (AGENTS.md → Format check), run the format check on the files this branch changed against the
   integration branch (`<base>`):

   ```bash
   git diff --name-only --diff-filter=d <base>...HEAD \
     | grep -E '<the extensions your formatter owns>' \
     | xargs -r <format-check command>
   ```

   On failure, autofix the files this branch changed (the `git diff --name-only` list above) and
   commit the fix. Skip tool-generated artifacts (generated/release files, copied configs/locales)
   — those are regenerated, not hand-formatted.

2. **Sibling linkage (workspace runs only).** If `.agent/work/<TICKET>/repo-split.md`
   exists and lists linkage steps for THIS repo, apply those steps (quoted in the split)
   before running any gate, so every gate runs against the sibling branches the split
   names — a green gate against the released dependency is not evidence for a change
   that targets its branch. If a linkage step fails or cannot be applied, record every
   gate that depends on it as `BLOCKED-BY-LINKAGE` — an environment gap, never a pass.
   If `repo-split.md` is absent, skip this step silently (single-repo run).
3. Run the **verify gate** (lint + typecheck + unit tests — AGENTS.md → Stack & commands).
4. If the change touches UI / integration scope, run the **e2e gate** (AGENTS.md).
5. **Behavioral confirmation.** Added on top of the configured gates — it confirms the change
   _does what the ticket says_ when actually driven; it does not replace the committed tests.
   Identify the change's drivable runtime surface — UI screen, HTTP endpoint, CLI command,
   job/handler — and exercise the acceptance-criteria paths through it THIS session, including
   the failure paths the ticket names, via the means AGENTS.md configures (run the app,
   browser-automation MCP, an HTTP client, the CLI itself). Record the actual output as the
   gate's evidence.
   - **Frontend changes**, with a **browser-automation MCP** configured (e.g. Playwright): boot
     the app (AGENTS.md → run the app), navigate to the changed screen, perform the relevant
     interactions, and screenshot at the design's viewport. Then a **structured visual review**
     against `.agent/work/<TICKET>/assets/`: report concrete deviations — spacing, color,
     typography, alignment, sizing, missing/extra elements (e.g. `Button padding 12px vs design
     16px — FIX`). This is a review, not a byte-level diff. No design asset → downgrade the
     visual half to "renders correctly / not visually broken".
   - Hand deviations back to `implement-plan`; re-confirm until clean.
   - **Degrade honestly:** if the means to drive the surface is missing (no browser-automation
     MCP for a UI check, the app cannot boot in this environment), record the gate as
     `BLOCKED-BY-ENV` — never as a pass. If the change has no drivable runtime surface (pure
     library/type-level change), record `NO-RUNTIME-SURFACE` with a one-line justification —
     the unit gates carry it.
6. Record pass/fail per gate. On failure, summarize the failures and hand back to
   `implement-plan` — do not open a PR with a failing gate.

## Discipline: verification before completion (mandatory)

**Iron law: no PASS without fresh evidence in this run.** If you didn't run the command this
session and read its output, you cannot mark the gate green.

Each gate's verdict needs the right evidence — not a proxy for it (exact commands per AGENTS.md →
Stack & commands):

| Claim        | Requires                                | Not sufficient   |
| ------------ | --------------------------------------- | ---------------- |
| Lint clean   | the lint command exits 0                | "should be fine" |
| Typecheck OK | the typecheck command exits 0           | lint passed      |
| Unit pass    | the unit run reports 0 failures         | a previous run   |
| e2e pass     | the e2e run reports 0 failures          | unit passed      |
| Behavioral OK | the AC paths exercised against the running change this run, output recorded | every gate passed |
| Visual OK    | screenshot reviewed vs `assets/`        | the app booted   |
| Linkage OK   | the split's linkage commands exited 0 this run | mocks passed     |
| Bug fixed    | the original symptom now passes         | the code changed |

- Evidence before assertions: run the command, then record the ACTUAL output. Never write that a
  gate passed unless you ran it and saw it pass.
- Distinguish honestly between PASS / FAIL / BLOCKED-BY-ENV — e.g. missing browser binaries is an
  environment gap, not a code failure; label it as such.
- The validation report reflects real per-gate results — no aspirational "should pass".

## Output artifact: `.agent/work/<TICKET>/validation-report.md`

Per-gate result (format / lint / typecheck / unit / e2e / behavioral-confirmation — the visual
review included for frontend changes), failures quoted, final verdict `PASS` / `FAIL`.

REQUIRED closing section — the **Attempt log**, append-only across the run's verify passes.
When rewriting the report, never overwrite this section; append one row per pass:
attempt # · verdict · failure signature (failing test/rule name, one line) · what the implement
pass since the previous attempt changed to address it (from `git log`). Fresh subagents have no
memory: this log is the only way the next implement dispatch knows what was already tried —
without it, every retry is attempt #1.

REQUIRED closing section — **Untested behaviors**: one row per untestable-behavior claim the
implement stage recorded (behavior · named obstacle · what would verify it), plus every behavior
whose only verification is a script outside the configured gates — with its exact run command —
or the single word "none". Run each such un-gated script THIS session and record its output with
the gate results (un-gated is not un-run). This section exists so `pr-review` can judge each
claim; a claim that never reaches this report evaporates.
