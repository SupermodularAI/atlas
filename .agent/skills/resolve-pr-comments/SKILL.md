---
name: resolve-pr-comments
description: "Use to drive an existing PR's unresolved review threads to resolution — triage, write a gated resolution plan, and on approval apply edits, reply, and resolve threads."
argument-hint: '[PR-NUMBER]'
---

# Resolve PR Comments

Drive an existing PR's unresolved review threads to resolution. **Plan first, human-gated:**
never write to the forge (push, reply, resolve) before the human approves the plan.

## Forge primitives

The forge is the one configured in AGENTS.md → Tooling (Code forge row). This skill needs exactly
three primitives from it; the commands below implement each with **GitHub via the `gh` CLI** as
the worked example. On another forge (e.g. GitLab via `glab`), fill these three slots with its
equivalents — the procedure is otherwise identical:

1. **List a PR's unresolved review threads**, each with a thread id, path/line, author, and body.
2. **Reply in-thread** to a given thread (never as a top-level PR comment).
3. **Resolve a thread** by its id.

## Input

- A PR number (e.g. `886`). Derive the repo identity from the forge CLI (e.g.
  `gh repo view --json owner,name`).

## Procedure

1. **Fetch unresolved threads** (primitive 1). GitHub example, via the GraphQL API:

   ```bash
   gh api graphql -f query='
     query($owner:String!,$repo:String!,$pr:Int!){
       repository(owner:$owner,name:$repo){
         pullRequest(number:$pr){
           reviewThreads(first:100){
             nodes{ id isResolved isOutdated path line
               comments(first:30){ nodes{ databaseId author{login} body } } } } } } }' \
     -F owner=<owner> -F repo=<repo> -F pr=<n>
   ```

   Keep only `isResolved == false`. Record each thread's `id` (node id, for resolving),
   first comment `databaseId` (for replying), `path`, `line`, author login, and body.

2. **Triage** each thread:

   - mechanical-bot (formatter / linter / static-analysis bot) vs human;
   - blocking vs suggestion;
   - clear vs ambiguous — if a thread's ask is ambiguous, do not guess a fix: mark it
     `NEEDS-CLARIFICATION` in the plan and raise it at the approval gate.

3. **Write the plan** to `.agent/work/PR-<n>/comment-resolution-plan.md`, grouped (bot threads,
   human threads, PR-level). For each thread record:

   - **Decision** — what we agreed to do (accept / push back, with reasoning);
   - **Action** — the concrete code change, held for approval (or "none — reply only");
   - **Reply** — the drafted thread response: state the technical resolution ("Fixed: <what>" or
     "Kept as-is because <evidence>"). No "You're absolutely right", no thanks — replies post
     in-thread (the reply endpoint below), never as a top-level PR comment.
     End with an **Execution log** checklist.

4. **STOP.** Present the plan and await human approval. **No forge writes before this gate.**

5. **On approval, execute in order:**
   - Apply the code edits. Verify each finding against the actual code first — neither blindly
     apply nor blindly dismiss (see `implement-plan` → Receiving code review). For a wrong finding,
     reply with evidence instead of a cosmetic change.
   - Run the `verify-changes` gates (format check + verify gate); do not push a failing gate.
   - Commit your code edits (`commit-message` skill, `chore(PR-<n>)`), then `git push`. **Never
     commit `comment-resolution-plan.md`** or anything under `.agent/work/` — artifacts are local
     working files, kept out of git (stage by explicit path, not `git add -A`).
   - Post one reply per thread (primitive 2; GitHub example):
     ```bash
     gh api --method POST \
       repos/<owner>/<repo>/pulls/<n>/comments/<comment_databaseId>/replies \
       -f body='<reply text>'
     ```
   - Resolve each addressed thread (primitive 3; GitHub example):
     ```bash
     gh api graphql -f query='
       mutation($id:ID!){ resolveReviewThread(input:{threadId:$id}){ thread{ isResolved } } }' \
       -f id=<thread_node_id>
     ```
   - Bot threads that auto-outdate on the next CI run: note in the log, do not manually resolve.
   - Tick the execution log.

## Output

`.agent/work/PR-<n>/comment-resolution-plan.md`, and — only after approval — pushed edits,
posted replies, and resolved threads.

## Out of scope

No auto-posting without the gate. Does not open or merge PRs.
