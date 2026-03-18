---
description: Review and process open Dependabot PRs with automated lint, test, changelog analysis, and approval workflow
---

# Dependabot PR Review

Automate the review of open Dependabot pull requests. For each PR: check out the branch, run lint/test, analyze the changelog against codebase usage, fix issues, and present a summary for approval.

## Step 1: Discover open Dependabot PRs

Run:
```
gh pr list --repo speakeasy-api/gram --author 'app/dependabot' --state open --json number,title,headRefName,url
```

If there are no open Dependabot PRs, inform the user and stop.

Display the list of discovered PRs to the user before proceeding.

## Step 2: Process PRs in parallel batches

Process PRs in batches of **3 at a time**. For each PR, spawn a sub-agent using the Agent tool with `isolation: "worktree"` and `subagent_type: "general-purpose"`.

Pass each sub-agent the following prompt (fill in the PR-specific values):

---

**You are reviewing Dependabot PR #NUMBER: "TITLE"**
**Branch:** BRANCH_NAME
**URL:** PR_URL

Activate the `golang` skill if this PR touches Go code.

### Tools available

This project uses `mise` as its task runner. Run `mise tasks` to discover available commands for linting, testing, building, and more. Use `mise run <task> --help` for details on any task.

### Setup

1. Fetch and check out the PR branch:
   ```
   git fetch origin BRANCH_NAME
   git checkout BRANCH_NAME
   ```

2. Identify what changed:
   ```
   git diff main..HEAD --name-only
   ```

### Analyze dependency changes

1. Parse the diff to extract which dependencies changed and their old→new versions:
   - For Go: diff `go.mod` between main and HEAD
   - For npm: diff `package.json` files between main and HEAD
   - For Docker: diff Dockerfiles between main and HEAD
   - For GitHub Actions: diff `.github/workflows/` between main and HEAD

2. For each changed dependency, determine the GitHub repository (from the module path or package registry).

### Run lint and test (in parallel where possible)

Based on what files changed, run the appropriate checks:
- Go files changed → run `mise lint:server` and `mise test:server` in parallel (use parallel Bash calls)
- Client files changed → run `mise lint:client`
- CLI files changed → run `mise lint:cli`
- Functions files changed → run `mise lint:functions`

Record pass/fail for each.

### Fetch and analyze changelog

For each changed dependency:

1. **Try GitHub Releases first:**
   ```
   gh api --method GET repos/{owner}/{repo}/releases --jq '.[].tag_name' 2>/dev/null
   ```
   Filter releases between the old and new version tags. Fetch the body of relevant releases.

2. **Fall back to CHANGELOG.md:**
   ```
   gh api --method GET repos/{owner}/{repo}/contents/CHANGELOG.md --jq '.content | @base64d' 2>/dev/null
   ```
   Extract the section between old and new versions.

3. If neither works, record "no changelog found".

4. **Record the changelog URL** for each dependency — either the GitHub Releases page (`https://github.com/{owner}/{repo}/releases`) or a direct link to the CHANGELOG.md (`https://github.com/{owner}/{repo}/blob/main/CHANGELOG.md`).

### Analyze impact on our codebase

1. Search for all imports and usages of the changed dependency in our codebase:
   - For Go: `grep -r "import_path" server/ cli/ functions/`
   - For npm: grep for the package name in `client/` and `elements/`

2. Cross-reference changelog entries against our usage. Categorize the risk:
   - **breaking**: A public API, type, function, or interface we import/use has changed, been removed, or had its signature modified
   - **deprecation**: Something we use is marked deprecated
   - **internal**: Changes are internal to the dependency and don't affect any API surface we consume
   - **unknown**: No changelog available to assess

3. **Be conservative**: If we consume ANY interface, type, or function mentioned in the changelog — even if the change "shouldn't" affect our usage in principle — flag it as needing review. Err on the side of caution.

4. **Highlight opportunities**: Look for new features, APIs, or improvements in the changelog that could benefit our codebase. For example: new utility functions we could adopt, performance improvements we could opt into, or new configuration options that would be useful. Note these as `opportunities` in your result.

### Fix issues

If lint or tests fail:
- Attempt straightforward fixes (import path changes, renamed functions, updated API signatures)
- After making Go changes, run `mise run go:tidy` to clean up module dependencies
- Run `hk fix --all` to auto-fix formatting and lint issues
- **Do NOT commit or push.** Leave your changes staged/unstaged — the supervising agent will handle committing and pushing.
- If the fix is non-trivial or uncertain, do NOT attempt it — flag it for human review instead

### Return your findings

Return a **single structured summary** in exactly this format (the supervising agent will parse this):

```
DEPENDABOT_RESULT_START
pr_number: NUMBER
pr_title: TITLE
pr_url: URL
worktree_path: /path/to/worktree
branch: BRANCH_NAME
lint_passed: true/false
test_passed: true/false
fixes_committed: true/false
dependencies:
  - name: DEPENDENCY_NAME
    old_version: X.Y.Z
    new_version: A.B.C
    risk_tier: breaking/deprecation/internal/unknown
    changelog_url: https://github.com/{owner}/{repo}/releases (or CHANGELOG.md link)
    changelog_summary: One-line summary of changes
    concerns:
      - Specific concern about API/interface we consume
      - Another concern
    opportunities:
      - New feature/API we could adopt and how it would help
    our_usage:
      - file:line where we import/use this dep
recommendation: approve/needs-review
recommendation_reason: Brief explanation
DEPENDABOT_RESULT_END
```

---

## Step 3: Compile results

After all sub-agents complete, compile their results into a summary table:

```
| # | PR | Dependency | Version | Lint | Test | Risk | Fixes | Recommendation |
|---|-----|------------|---------|------|------|------|-------|----------------|
```

Below the table:
- For each dependency, include a link to the changelog (GitHub Releases page or CHANGELOG.md).
- For any PR with `opportunities`, list them so the user can consider follow-up work.
- For any PR flagged `needs-review`, list the specific concerns.

## Step 4: User confirmation

Use AskUserQuestion with `multiSelect: true` to ask:
> "Which PRs should be approved and pushed?"

List each PR as an option with its recommendation. PRs marked `needs-review` should include "(Needs Review)" in their label.

## Step 5: Commit and push approved PRs

For each approved PR where the sub-agent made fixes:
1. Stage and commit changes from the worktree: `git -C <worktree_path> add -A && git -C <worktree_path> commit -m "fix: resolve compatibility issues with <dep> upgrade"`
2. Push to the PR branch: `git -C <worktree_path> push origin <branch>`
3. Output the PR URL for the user to merge

For PRs not approved, just note them as skipped.

## Step 6: Clean up all worktrees

After pushing (or skipping), remove ALL worktrees created during this process:
```
git worktree remove --force <worktree_path>
```

Do this for every worktree, whether the PR was approved or not.

Provide a final summary: how many PRs were pushed, how many skipped, and links to the pushed PRs.
