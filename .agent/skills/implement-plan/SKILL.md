---
name: implement-plan
description: 'Use as the implementation pipeline stage — execute an implementation plan on a feature branch, following repo conventions and committing in small steps.'
---

# Implement Plan

Implementation stage. Execute `.agent/work/<TICKET>/implementation-plan.md`. Follow AGENTS.md exactly.

## Procedure

1. Ensure you are on a `<type>/<TICKET>/<slug>` branch, cut from the base named in AGENTS.md →
   Branch & commit policy (the integration branch, or a dependency branch if the context bundle
   says so). `<type>` mirrors the Conventional Commit type of the ticket's primary change
   (`feat`, `fix`, `chore`, …) — not feature-only. **If a branch for this ticket already exists**
   (locally or on a remote), it is a previous attempt's progress: check it out and resume from
   its tip AS-IS — never delete it, force-recreate it (`checkout -B`, `reset --hard`), rebase
   it, or start a parallel branch. If its base has drifted (the integration branch moved since
   it was cut), note the drift in your report and keep building on the tip — rewriting history
   is the human's call, not yours.
2. Work the plan step by step using **Test-Driven Development** (see Disciplines below).
3. Follow the project coding conventions in AGENTS.md (language strictness, naming, styling/theming,
   accessibility, import order). Never touch the off-limits paths declared there.
4. Commit frequently using the `commit-message` skill format. Stage by explicit path — never
   `git add -A`/`git add .` — and never stage or commit anything under `.agent/work/`: artifacts
   (including the plan and its ticked checkboxes) are local working files, kept out of git.
5. Expect to be re-dispatched with a failing validation report or with `pr-review` findings —
   the loop is owned by whoever drives the stages (orchestrator or human). On a failing report,
   apply **Systematic debugging**; on findings, apply **Receiving code review** (see below).

## Disciplines (mandatory)

Non-negotiable rules for this stage — identical across tools. Violating the letter of these
disciplines is violating their spirit — a clever reading that skips the work still skips it.

### Test-Driven Development

**Iron law: no production code without a failing test first. Wrote code before the test? Delete it
and start from the test.** "Skip TDD just this once" / "too simple to test" / "I'll test after" is
rationalization, not an exception.

- Write the failing test FIRST for the specific behavior; run it; confirm it fails for the
  right reason (not a setup/typo error).
- Write the MINIMAL code to make it pass; run; confirm green.
- Refactor only with tests green.
- Honor the project testing strategy (AGENTS.md → Testing strategy): write tests only where that
  strategy places them, and use the framework it names for each layer.
- **"Cannot be tested" is a claim, not an exemption.** A claim must name the concrete structural
  obstacle; "hard to test", time pressure, or a colleague's say-so never qualify — and a tool the
  strategy names (e.g. fake timers) makes the behavior testable, period. Record every genuine
  claim for the validation report's **Untested behaviors** section (behavior · obstacle · what
  would verify it); a claim living only in a code comment or the PR text does not exist. TDD
  still applies to every testable part of the task.
- **No test framework configured at all?** TDD does not lapse: verify with the runtime's built-in
  test capability (e.g. `node --test`, a plain assert script) — zero new dependencies. Never
  install a framework, add package scripts, or widen the verify gate unilaterally; that is the
  maintainer's decision — surface it as a follow-up. List the behavior in Untested behaviors
  with its exact run command, noting the verification is not wired into the verify gate.
- Follow the project's test-file naming/colocation rule (AGENTS.md → Testing strategy). If a test
  file already exists under a different name, rename it to match the source (grep for and update
  references to the old name) and add to it; flag the rename in the PR.

### Systematic debugging (when any gate or test fails)

**Iron law: no fix without root-cause investigation first.** If you're thinking "quick fix for
now", "just try X and see", "it's probably X", or "I don't understand it but this might work" —
STOP: you have not found the root cause yet.

- **First, read the Attempt log** in `.agent/work/<TICKET>/validation-report.md` — you may be a
  fresh dispatch with no memory of earlier attempts; the log is that memory. Never re-try a
  hypothesis the log records as already failed.
- Reproduce the failure deterministically before changing anything.
- Read the actual error and trace the ROOT cause — do not guess-and-patch.
- Fix the cause, not the symptom. No masking: no swallowed errors, no arbitrary waits, no
  loosened assertions to turn red green.
- Re-run the gate to confirm the fix.
- After **3 failed fixes — counted from the Attempt log, not your own recollection — or when each
  fix reveals a new problem elsewhere — STOP and report**: the architecture is likely wrong. Name
  the pattern you suspect is unsound; do not attempt fix #4.

### Plan ↔ reality (any dispatch, any discovery — not only during review)

When the repo contradicts the plan's text — a file, symbol, path, signature, or value the plan
names is absent or different in the actual repo, including a plan step that would create a
parallel version of something that already exists — classify by whose truth is violated:

- **The ticket also names it** (the mismatched name/path/value appears in the context bundle's
  requirement or acceptance-criteria text): the ticket's premise is broken and the human owns
  it — STOP and report the plan text beside the repo reality. Never resolve it yourself.
- **Only the plan names it**: the plan is wrong about the repo — repair it. Build against what
  actually exists, and record it in `implementation-plan.md` under `## Deviations`
  (`plan said · repo has · did`) in the same step, before moving on. A deviation living only in
  your final report or a commit message does not exist: downstream stages read the plan, not
  your transcript.

Everything the plan does not name is added detail and is yours to fold in.

### Receiving code review (acting on `pr-review` findings)

- Verify each finding against the actual code before acting — neither blindly apply nor blindly
  dismiss.
- No performative agreement — never "You're absolutely right" / "Great point" / "Thanks for
  catching". Verify, then either state the fix ("Fixed: <what>") or push back with evidence.
- If a finding is wrong, push back with evidence instead of making a cosmetic change.
- If a finding contradicts the implementation-plan's text, apply **Plan ↔ reality** (above).
- After fixing, re-run the gates and update the validation report.

### Verify your own claims

- **No "it passes" without having run it in this turn and seen 0 failures.** Evidence before
  assertions (full rules + the evidence table in the `verify-changes` skill).

## Output

Code + commits on the feature branch; plan checkboxes ticked.
