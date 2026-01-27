---
description: Refactor code while maintaining functionality
---

# Code Refactoring

Refactor the specified code to improve quality while preserving functionality.

## Process

1. **Understand the current code**: Read and analyze the code to be refactored
   - Identify the current behavior and contracts
   - Note any tests that cover this code
   - Understand dependencies and callers

2. **Run existing tests** to establish baseline:
   - Go: `mise test:server -- ./path/to/package`
   - TypeScript: `pnpm test`

3. **Identify refactoring opportunities**:

   ### Complexity Reduction
   - Extract deeply nested code into helper functions (4+ levels is too deep)
   - Replace complex conditionals with early returns (line-of-sight principle)
   - Break large functions into smaller, focused functions

   ### Code Organization
   - Group related functionality
   - Separate concerns (data access, business logic, presentation)
   - Extract reusable utilities

   ### Pattern Improvements
   - Apply Go idioms (error handling, interface usage)
   - Apply React patterns (custom hooks, component composition)
   - Use existing project abstractions consistently

   ### Gram-Specific Patterns
   - Ensure services follow the standard struct pattern with tracer, logger, db, repo
   - Use `oops` package for error handling in Go services
   - Use `@tanstack/react-query` for data fetching in React
   - Use Moonshine design system utilities for styling

4. **Apply refactoring incrementally**:
   - Make one change at a time
   - Verify tests still pass after each change
   - Commit or checkpoint working states

5. **Run linters** after refactoring:
   - `mise lint:server` for Go
   - `mise lint:client` for TypeScript

6. **Verify functionality**:
   - Re-run all tests
   - If applicable, test manually in development environment

7. **Document changes**: Explain what was refactored and why in your response

## Important Notes

- **Preserve external contracts**: Don't change function signatures, API endpoints, or public interfaces unless explicitly requested
- **Keep PRs focused**: Refactoring PRs should be separate from feature PRs (Boyd's Law)
- **Don't over-engineer**: Only refactor what's necessary; avoid premature abstractions
- **Generated files**: Never refactor files in `server/gen/` or with "DO NOT EDIT" headers

## Arguments
- `$ARGUMENTS`: File path, function name, or package to refactor
