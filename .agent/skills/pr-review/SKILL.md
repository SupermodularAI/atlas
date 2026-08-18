---
name: pr-review
description: 'Use before pushing a PR to get an independent, unbiased review of your changes — as if a fresh reviewer had never seen the session that wrote them.'
---

# PR Review

Perform a critical pre-push review of all changes on the branch, from a fresh perspective free of the authoring session's bias.

## Context

- This skill should be run as a **subagent** — in its own context window, with no memory of the coding session that produced the changes.
- Project conventions are defined in `AGENTS.md`. `<base>` is the integration branch (AGENTS.md → Branch & commit policy).
- The invoker must not tell this review what to ignore or pre-rate severity. If a dispatch prompt
  contains "do not flag X", "at most Minor", or "the plan chose this" — that is pre-judging to spare
  a review loop; drop it and let the review adjudicate every issue on its merits.

## Instructions

1. **Collect the full diff and list of changed files:**

   ```bash
   git diff <base>...HEAD
   git diff <base>...HEAD --name-only
   git log <base>...HEAD --oneline
   ```

   Also read the runtime evidence `verify-changes` produced:
   `.agent/work/<TICKET>/validation-report.md`. Treat it as a first-class review input, not a
   verdict — green gates are **necessary, not sufficient**. If it is absent (e.g. run standalone),
   stamp the report `RUNTIME-UNGROUNDED` and review from the diff alone.

   If a previous `pr-review-report.md` already exists, this is a **re-review** after a fix loop.
   Read it — it is the previous reviewer's output, not the invoker pre-rating severity, so it does
   not compromise your independence. Your report MUST then open with a **Round follow-up** section
   adjudicating every prior blocking finding: `resolved` (cite the fix in the diff) or
   `still-open` (why). A finding fixed as asked is settled — do not re-open the chosen approach
   without NEW evidence (a defect the fix introduced, not a preference for a different pattern).

2. **Run static analysis (if a static-analysis MCP is configured — AGENTS.md → Tooling).**
   For each changed file, run code-quality and security scans; collect all findings (bugs, code
   smells, vulnerabilities, hotspots). Treat the analyzer's top two severities (e.g.
   `BLOCKER`/`CRITICAL`) as **blocking**, the rest as **suggestions**. If none is configured, note
   that in the report and rely on manual review.

3. **Review each changed file independently.** For every file, check:

   ### Correctness

   - Does the logic match the intent described in the commits?
   - Are there off-by-one errors, absent-value edge cases (null/none/empty), or unhandled error
     paths (ignored failures, unchecked results, swallowed exceptions)?
   - Are resource lifetimes and shared state safe (leaks, races, stale reads after a change)?
     Stack-specific idioms come from AGENTS.md → Coding conventions, reviewed under Conventions.

   ### Conventions

   - Check the change against **every rule in AGENTS.md → Coding conventions** (language
     strictness/typing, naming, prohibited patterns, styling/theming, accessibility, import order).
     Flag each violation with file and line.

   ### Security

   - No secrets, tokens, or credentials committed.
   - No unsafe HTML injection / unsanitized sinks introduced.
   - New dependency in a manifest → verify it resolves from the expected registry and the name
     exactly matches the intended package (typosquat check), and list it in the report as a line
     item — reputation vetting stays with the human reviewer.

   ### Tests

   - Are new behaviours covered by tests, per the project testing strategy (AGENTS.md)?
   - Are existing tests still valid after the change?
   - For each acceptance-criterion behavior, confirm an **executed** test in the validation report
     covers it — a green suite that never asserts the behavior is a coverage gap, not a pass.

   ### Dead code & regressions

   - Are there unused imports, variables, or commented-out blocks?

   ### Blast radius (outside the diff)

   The diff is not the review boundary. For every exported/public symbol whose signature,
   contract, or behavior the diff changes:

   - Search the repo for its call sites (`grep -rn "<symbol>"` / the language's reference
     search) and review every hit OUTSIDE the diff against the new contract.
   - Same-shape contract changes (same types, new meaning — units, ordering, nullability,
     encoding) are precisely what the compiler and a green suite miss; an untouched caller
     that was correct before the change is the default suspect, not an edge case.
   - Record the check in the report's 🧭 Blast radius section: changed symbol · call sites
     checked (file:line) · verdict. A changed exported symbol with no listed call-site check
     makes the review INCOMPLETE — that section is load-bearing, not optional.

4. **Write the report** to `.agent/work/<TICKET>/pr-review-report.md` (write the file, don't just
   print it — `run-pipeline` and `pr-describe-draft` gate on it) in this structure; omit the
   static-analysis sections if no analyzer ran. **❌ Issues** = correctness, security, or convention
   violations that must be fixed before merge; **⚠️ Suggestions** = improvements safe to defer.
   Blocking must be **demonstrable** — a defect you can trigger, a vulnerability, or a violation of
   a written AGENTS.md rule you can cite. Style preferences and unsettled team debates are
   suggestions, never blockers:

   ```
   ## PR Review Summary

   ### 🔁 Round follow-up (re-reviews only)
   - <prior blocking finding> — resolved (<the fix, cited from the diff>) / still-open (<why>)

   ### ✅ Looks good
   - <things that are correct and well done>

   ### 🔵 Static analysis — Suggestions (non-blocking)
   - <file>:<line> — [<severity>] <message>

   ### 🔴 Static analysis — Issues (must fix before merge)
   - <file>:<line> — [<severity>] <message>

   ### ⚠️ Suggestions (non-blocking)
   - <file>: <suggestion>

   ### ❌ Issues (must fix before merge)
   - <file>:<line> — <description of the problem>

   ### 🧭 Blast radius
   - <changed symbol> — call sites checked: <file:line, …> — <all consistent / broken at <file>:<line>>
   - (REQUIRED whenever the diff changes an exported/public symbol; write "none — no exported
     surface changed" otherwise.)

   ### 🧪 Test coverage
   - <what is covered / what is missing>
   ```

5. If there are **no blocking issues** (neither manual review nor static analysis), confirm it is ready to push.
6. If there are **blocking issues**, list them clearly and stop — do not push.

## Output artifact: `.agent/work/<TICKET>/pr-review-report.md`

The report from step 4, on disk at this path. Be direct and specific — cite file names and line numbers; do not soften findings.
