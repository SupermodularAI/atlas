---
name: pr-describe-draft
description: 'Use as the draft-PR pipeline stage — when a branch has passed verification and review and is ready to publish as a draft pull request.'
---

# PR Describe

Create structured draft pull request descriptions that provide context for reviewers.

## Context

- Commit format uses Conventional Commits: `type: message`.
- Use the repo's PR/MR template for the section structure, if present — resolve it from the
  forge's conventional location (e.g. GitHub: `.github/pull_request_template.md`; GitLab:
  `.gitlab/merge_request_templates/`).
- `<base>` is the integration branch (AGENTS.md → Branch & commit policy). The forge CLI is the
  one configured in AGENTS.md → Tooling (e.g. GitHub `gh`).

## Instructions

0. **Gate before any push.** Confirm `.agent/work/<TICKET>/validation-report.md` exists and reads
   `PASS`, and `pr-review-report.md` has no blocking findings. If it is missing, reads `FAIL`, or
   has blocking findings, STOP — do not push or open a PR.
1. **Read the full diff and commit log:**
   - `git log <base>...HEAD --oneline` for all commits on this branch.
   - `git diff <base>...HEAD` for the complete changeset.
   - Understand the purpose of the change, not just the mechanics.
2. **Generate the PR title:**
   - Use Conventional Commit format: `type: message`.
   - Keep under 70 characters.
   - Use the most significant type (feat > fix > chore/test/docs).
3. **Fill every section of the PR template.** Typical sections:
   - **Description**: 2–5 sentences or bullets explaining what changed and why — motivation and
     approach, not a file-by-file list. Mention design decisions or trade-offs.
   - **Related Issue**: link to the tracker ticket from the commits or branch name.
   - **How to Test**: step-by-step instructions for the reviewer.
   - **Screenshots**: leave empty for the engineer to add.
   - **Checklists**: leave empty for the engineers/reviewers to check.
   - **Related PRs & merge order** (REQUIRED when `.agent/work/<TICKET>/repo-split.md`
     exists; omit the section entirely otherwise): add it even though the template lacks
     it. List every sibling PR the split names, in the split's work order — URL where
     known, else `<repo>: PR pending` — and state that this order is the merge order.
     The workspace orchestrator completes missing URLs in its cross-link pass.
4. **Push the branch and open the draft PR.** `pr-review` was the pre-push gate, so this
   is the first push. Push the branch (`git push -u origin HEAD`), then create the **draft**
   PR/MR with the title and body using the configured forge CLI (AGENTS.md → Tooling) and its
   draft flag — e.g. with GitHub:

   ```bash
   gh pr create --draft --title "<title>" --body "$(cat <<'EOF'
   <full PR body>
   EOF
   )"
   ```

## Output

A pushed branch and an open **draft PR** built from the generated title + body (every template
section filled). Report the PR URL. The title/body text is also emitted for the audit trail.

### Example of PR description

For branch `feat/PROJ-1234/add-export-button`.

```markdown
## Description

Adds an "Export CSV" action to the reports toolbar. The export is generated client-side from the
already-loaded report rows, so it adds no new request and reuses the existing formatting helpers.

## Related Issue

[PROJ-1234](https://your-tracker.example.com/browse/PROJ-1234)

## How to Test

1. Open a report with at least one row.
2. Click "Export CSV" → a file downloads containing exactly the visible rows and columns.
3. Apply a filter, then export again → only the filtered rows are included.
4. Open a report with no rows → the button is disabled.

## Screenshots

<!--- Add a screenshot or gif of the change. -->

## Checklist

<!--- Fill in the boxes that apply. -->
```
