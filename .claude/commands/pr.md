---
description: Create a Pull Request for the current changes and/or branch
---

# Creating a Pull Request

Create a Pull Request for the current changes and/or branch:

1. Ask user what the scope of the changes is for context
2. **Ensure repository is forked**: Check if the user has forked the repository. If not, create a fork using `gh repo fork` and set up the remote properly
3. Use the current branch or create a new branch if on main
4. **Validate conventional commit prefix**: Ensure PR title and any commits use proper conventional commit prefixes:
   - `chore:` - for maintenance tasks, configuration changes, tooling updates
   - `feat:` - for new features or functionality additions
   - `fix:` - for bug fixes and error corrections
   - `mig:` - for database migrations and schema changes
   - Ask user to clarify the change type if unclear, and enforce one of these four prefixes
5. If there are uncommitted changes, create a single line conventional commit summarizing the changes eg. `feat: add new feature`
6. **Scrub the branch**: Before pushing, check the branch name and every commit message on the branch against the rules in [Public metadata hygiene](#public-metadata-hygiene) below. Fix anything that fails before continuing — a push publishes both.
7. Ensure the branch is up-to-date with remote
8. Push the branch to the user's fork (origin remote)
9. Create a short but well-structured PR description in a temporary file (with a unique file name) in the /tmp directory for reviewers
10. **Scrub the PR metadata**: Check the PR title and the drafted description against the same rules. Fix them in place before opening the PR — once created, they are public.
11. Create PR with the description file using GitHub CLI, ensuring the title starts with `chore:`, `feat:`, `fix:`, or `mig:` and targets the upstream repository
12. Add appropriate labels based on change type (enhancement, bug, documentation, etc.). Ensure to query for the available labels beforehand.
13. Provide a clickable link to the PR in the output

Follow conventional commit standards and keep PR descriptions concise but informative. **IMPORTANT**: All PR titles MUST start with one of the four prefixes: `chore:`, `feat:`, `fix:`, or `mig:`.

## Public metadata hygiene

This repository is public. Branch names, commit messages, PR titles, PR descriptions, PR comments, labels, and any attached screenshots or GIFs are permanently visible to anyone, and are mirrored to forks, CI logs, notification emails, and mailing-list archives. Deleting them later does not unpublish them.

**Never** include the following in public metadata:

- **Customer or company names** — including prospects, trial accounts, and companies mentioned only as the source of a bug report. This applies to org slugs, project slugs, and subdomains derived from a customer name.
- **Database identifiers** — org, project, deployment, toolset, chat, or user UUIDs; primary keys; any id copied out of Postgres or ClickHouse.
- **WorkOS identifiers** — `org_*`, `user_*`, `directory_*`, `conn_*`, or any other WorkOS-issued id.
- **External service identifiers or credentials** — API keys, tokens, session ids, webhook URLs, Temporal workflow/run ids, GCP project ids or resource names, Datadog/PostHog/Slack ids, and internal-only URLs that embed any of the above.
- **PII** — real names, email addresses, IP addresses, phone numbers, or any user-authored content (prompts, chat transcripts, tool arguments) captured from a real account.
- **Anything else sensitive** — unannounced customers or partnerships, contract or pricing detail, unreleased plans, security findings that are not yet fixed.

This applies to the whole diff too, not only the metadata around it: no sensitive values in fixtures, seed data, test snapshots, hardcoded constants, or code comments.

### How to say it instead

Describe the shape of the problem, not the identity behind it. Reviewers need the reproduction conditions, not the account.

| Instead of                            | Write                                                       |
| ------------------------------------- | ----------------------------------------------------------- |
| `fix: Acme's toolset fails to load`   | `fix: toolset fails to load when a deployment has no tools` |
| "Reported by Acme Corp"               | "Reported via support (see AGE-1234)"                       |
| "org `org_01H8X...` sees 500s"        | "orgs with >1000 tools see 500s"                            |
| "chat `9f3c1a2e-...` reproduces this" | "any chat with an empty transcript reproduces this"         |
| "jane@acme.com hit this"              | "a user with a directory-synced account hit this"           |

Link a Linear issue when the private context matters — Linear is the right home for customer names and ids. `AGE-1234` in the description gives reviewers the trail without publishing it.

Redact rather than omit when a real value is needed to explain a bug: `org_01H8X…` (truncated), `<org-id>`, or `00000000-0000-0000-0000-000000000000` in fixtures.

### Checking

Run the scan over everything the PR will publish, not just the working tree. At step 6 that is the branch name and the commits; at step 10 it is the title and description you have just drafted, read against the same rules.

```sh
# Branch name and every commit message that will land upstream.
git rev-parse --abbrev-ref HEAD
git log upstream/main..HEAD --format='%H%n%s%n%b'
```

Read the output yourself and judge it against the rules above — a regex cannot recognise a company name. As a supplementary pass, grep the commits and the diff for the mechanical patterns:

```sh
# UUIDs, WorkOS ids, and emails in commit messages or the diff.
git log upstream/main..HEAD --format='%s%n%b' | rg -i '\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b|\b(org|user|client|directory|conn)_[0-9A-HJKMNP-TV-Z]{26}\b|[\w.+-]+@[\w-]+\.[\w.]+'
git diff upstream/main...HEAD | rg -i '\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b|\b(org|user|client|directory|conn)_[0-9A-HJKMNP-TV-Z]{26}\b'
```

Expect false positives (migration ids, generated fixtures, test constants); use judgement rather than mechanically rewriting every hit.

### Fixing a violation

**Nothing pushed yet** — amend or rebase the offending commits (`git commit --amend`, `git rebase -i upstream/main`) before pushing. This is the cheap case, and the reason the scans run at steps 6 and 10, before anything is published.

**Not opened yet** — edit the title or the description file directly. Also cheap.

**Already disclosed on a PR** — the PR **must not be merged**. Follow the company remediation procedure, in order. Do not improvise around it, and do not attempt to salvage the PR by editing it in place:

1. Capture the diff of the PR by going to the PR page and adding `.diff` to the end of the URL. Save it to a file outside the repository and give the user the path.
2. **The user must notify their manager**, providing that diff and the affected customers. This step is theirs, not yours — you do not know who their manager is and must not attempt to contact anyone. Report what was exposed and which customers are affected, hand over the diff, and **stop there**.
3. Scrub the PR branch by rebasing and force-pushing.
4. Scrub the PR title and description (`gh pr edit`).
5. Verify: the remote branch has exactly one commit with no exposures, and the PR's "Changes" tab is clean.
6. Close the PR and delete the remote branch. Reopen the work as a fresh PR from a clean branch.

Steps 1 and 2 run in the same turn: capture the diff, then report and wait. Steps 3–6 only run once the user confirms.

**A credential was published** — treat it as compromised regardless of any scrub. Tell the user to rotate it first, before any history work.

When unsure whether something counts as sensitive, leave it out and ask the user. Omitting a detail costs a review round-trip; publishing one cannot be undone.

## Fork Requirements

All pull requests must be submitted from a personal fork of the repository. The command should:

- Check if a fork exists using `gh repo view --json parent` to determine if current repo is a fork
- If not a fork, create one with `gh repo fork --clone=false`
- Set up remotes properly: `origin` should point to the user's fork, `upstream` to the original repository
- Push changes to the fork and create the PR targeting the upstream repository
