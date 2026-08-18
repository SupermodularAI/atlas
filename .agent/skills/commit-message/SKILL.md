---
name: commit-message
description: 'Use when writing a commit message in a repo that follows Conventional Commits.'
---

# Commit Message

Generate a Conventional Commits message that passes the repo's commitlint rules.

## Rules

- Format: `<type>(<scope>): <subject>` — imperative present tense.
- Subject and body length limits come from the repo's commitlint config (AGENTS.md → Branch &
  commit policy). Common defaults: subject ≤ 50 chars, body wrapped at ≤ 72 chars per line.
- Blank line between subject and body; blank line before an optional footer (issue refs,
  `BREAKING CHANGE:`).
- Types: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `build`, `ci`, `perf`, `style`.

## Steps

1. Inspect changes: `git diff --cached` (fallback `git diff` if nothing staged).
2. Pick the most significant type and a scope from the touched package/app.
3. Write the subject within the configured limit. Count the characters.
4. Write the body: what changed and why, each line within the configured limit.
5. Verify line length: `awk '{ if (length > 72) print "TOO LONG:", NR }'` (use your configured limit).

## Output

The full commit message text, ready for `git commit -F -`.
