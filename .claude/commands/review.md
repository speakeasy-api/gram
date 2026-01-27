---
description: Review code for bugs, security issues, and improvements
---

# Code Review

Review the specified code (file, PR, or diff) for quality, bugs, security issues, and improvements.

## Process

1. **Identify the scope**: Determine what to review:
   - If a file path is provided: review that file
   - If a PR number is provided: fetch PR diff with `gh pr diff <number>`
   - If no argument: review staged changes with `git diff --cached` or recent commits

2. **Analyze for issues** in these categories:

   ### Security
   - SQL injection vulnerabilities
   - Command injection risks
   - Hardcoded secrets or credentials
   - Improper input validation
   - Missing authentication/authorization checks
   - Unsafe deserialization

   ### Bugs & Logic Errors
   - Null pointer / nil dereference risks
   - Off-by-one errors
   - Race conditions
   - Incorrect error handling
   - Missing edge cases
   - Resource leaks (unclosed connections, files)

   ### Gram-Specific Issues
   - **Go**: Missing context propagation, non-context slog methods, editing generated files
   - **Go**: Using `DELETE FROM` instead of soft deletes
   - **Go**: Missing `ON DELETE SET NULL` on foreign keys
   - **TypeScript**: Hardcoded Tailwind colors (should use Moonshine)
   - **TypeScript**: Manual `useEffect`/`useState` instead of `@tanstack/react-query`
   - **Database**: Missing timestamps, wrong constraint naming

   ### Code Quality
   - Functions with excessive nesting (4+ levels discouraged)
   - Missing or incorrect error handling
   - Inconsistent naming conventions
   - Missing tests for new functionality
   - Code duplication

3. **Run linters** based on changed files:
   - Go files: `mise lint:server`
   - TypeScript files: `mise lint:client`
   - CLI files: `mise lint:cli`

4. **Output format**: Organize findings by severity:
   ```
   ## Critical (must fix)
   - [file:line] Description of issue

   ## Warning (should fix)
   - [file:line] Description of issue

   ## Suggestion (nice to have)
   - [file:line] Description of improvement

   ## Positive Notes
   - Things done well
   ```

5. **Provide actionable fixes**: For each issue, suggest the specific fix

## Arguments
- `$ARGUMENTS`: File path, PR number, or empty for staged changes
