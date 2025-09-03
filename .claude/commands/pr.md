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
6. Ensure the branch is up-to-date with remote
7. Push the branch to the user's fork (origin remote)
8. Create a short but well-structured PR description in a temporary file (with a unique file name) in the /tmp directory for reviewers
9. Create PR with the description file using GitHub CLI, ensuring the title starts with `chore:`, `feat:`, `fix:`, or `mig:` and targets the upstream repository
10. Add appropriate labels based on change type (enhancement, bug, documentation, etc.). Ensure to query for the available labels beforehand.
11. Provide a clickable link to the PR in the output

Follow conventional commit standards and keep PR descriptions concise but informative. **IMPORTANT**: All PR titles MUST start with one of the four prefixes: `chore:`, `feat:`, `fix:`, or `mig:`.

## Fork Requirements

All pull requests must be submitted from a personal fork of the repository. The command should:
- Check if a fork exists using `gh repo view --json parent` to determine if current repo is a fork
- If not a fork, create one with `gh repo fork --clone=false` 
- Set up remotes properly: `origin` should point to the user's fork, `upstream` to the original repository
- Push changes to the fork and create the PR targeting the upstream repository
