---
description: Create a Pull Request for the current changes and/or branch
---

# Creating a Pull Request

Create a Pull Request for the current changes and/or branch:

1. Ask user what the scope of the changes is for context
2. Use the current branch or create a new branch if on main
3. **Validate conventional commit prefix**: Ensure PR title and any commits use proper conventional commit prefixes:
   - `chore:` - for maintenance tasks, configuration changes, tooling updates
   - `feat:` - for new features or functionality additions
   - `fix:` - for bug fixes and error corrections
   - `mig:` - for database migrations and schema changes
   - Ask user to clarify the change type if unclear, and enforce one of these four prefixes
4. If there are uncommitted changes, create a single line conventional commit summarizing the changes eg. `feat: add new feature`
5. Ensure the branch is up-to-date with remote
6. Push the branch to the remote origin
7. Create a short but well-structured PR description in a temporary file (with a unique file name) in the /tmp directory for reviewers
8. Create PR with the description file using GitHub CLI, ensuring the title starts with `chore:`, `feat:`, `fix:`, or `mig:`
9. Add appropriate labels based on change type (enhancement, bug, documentation, etc.). Ensure to query for the available labels beforehand.
10. Provide a clickable link to the PR in the output

Follow conventional commit standards and keep PR descriptions concise but informative. **IMPORTANT**: All PR titles MUST start with one of the four prefixes: `chore:`, `feat:`, `fix:`, or `mig:`.
