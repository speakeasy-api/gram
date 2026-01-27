---
description: Investigate and fix a bug
---

# Bug Investigation and Fix

Systematically investigate and fix the reported bug.

## Process

1. **Understand the bug report**:
   - What is the expected behavior?
   - What is the actual behavior?
   - What are the reproduction steps?
   - When did it start happening? (check recent commits if relevant)

2. **Reproduce the issue**:
   - Set up local environment if needed (`./zero`)
   - Follow reproduction steps
   - Capture error messages, stack traces, logs

3. **Investigate root cause**:

   ### Search for related code
   - Search for error messages in codebase
   - Find the code path that produces the bug
   - Check recent changes to related files with `git log -p --follow -- <file>`

   ### Common investigation patterns
   - **API errors**: Check Goa design files, then `impl.go` implementations
   - **Database issues**: Check queries in `queries.sql`, schema in `schema.sql`
   - **Frontend bugs**: Check React components, query hooks, state management
   - **Background job failures**: Check Temporal workflows in `server/internal/background/`

   ### Check logs and telemetry
   - Look for related errors in ClickHouse telemetry
   - Check Temporal workflow history for async issues

4. **Identify the fix**:
   - Determine minimal change needed
   - Consider edge cases and related code paths
   - Check if fix requires database migration

5. **Implement the fix**:
   - Make focused, minimal changes
   - Follow Gram coding conventions
   - Add defensive checks if appropriate

6. **Write regression test**:
   - Create test that would have caught this bug
   - Ensure test fails without fix, passes with fix
   - Go: Use table-driven tests with `require`
   - TypeScript: Use Vitest/Jest

7. **Verify the fix**:
   - Run related tests: `mise test:server` / `pnpm test`
   - Run linters: `mise lint:server` / `mise lint:client`
   - Manually verify if reproduction steps are available

8. **Document findings**:
   - Explain root cause
   - Explain the fix
   - Note any related issues or follow-up work

## Output Format

```
## Bug Summary
[Brief description of the bug]

## Root Cause
[Explanation of why the bug occurred]

## Fix Applied
[Description of the fix]

## Files Changed
- [file:line] - Description of change

## Regression Test
[Test that prevents recurrence]

## Verification
- [ ] Tests pass
- [ ] Linters pass
- [ ] Manual verification (if applicable)
```

## Arguments
- `$ARGUMENTS`: Bug description, error message, or issue reference
