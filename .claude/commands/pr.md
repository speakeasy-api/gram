---
description: Create a Pull Request for the current changes and/or branch
---

# Creating a Pull Request

Create a Pull Request for the current changes and/or branch:

1. Ask user what the scope of the changes is for context
2. Use the current branch or create a new branch if on main
3. If there are uncommitted changes, create a single line conventional commit summarizing the changes eg. `feat: add new feature`
4. Ensure the branch is up-to-date with remote
5. Push the branch to the remote origin
6. Create a short but well-structured PR description in a temporary file (with a unique file name) in the /tmp directory for reviewers
7. Create PR with the description file using GitHub CLI
8. Add appropriate labels based on change type (enhancement, bug, documentation, etc.). Ensure to query for the available labels beforehand.
9. Provide a clickable link to the PR in the output

Follow conventional commit standards and keep PR descriptions concise but informative.
