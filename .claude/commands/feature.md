---
description: Implement a feature from plan to PR
---

# Feature Implementation

Implement a feature following Gram's development workflow, from planning to pull request.

## Process

1. **Understand requirements**:
   - What problem does this feature solve?
   - Who are the users?
   - What are the acceptance criteria?
   - Are there related features or dependencies?

2. **Break down into atomic tasks** (Boyd's Law):
   - Identify smallest shippable increments
   - Each task should be independently verifiable
   - Order tasks by dependencies

3. **Plan implementation**:

   ### API Changes (if needed)
   - Design API in `server/design/` using Goa DSL
   - Define request/response types
   - Add security requirements
   - Run `mise gen:goa-server` to generate code

   ### Database Changes (if needed)
   - Modify `server/database/schema.sql`
   - Follow schema rules (soft deletes, timestamps, constraint naming)
   - Run `mise db:diff <feature-name>` to generate migration
   - Add queries to relevant `queries.sql` files
   - Run `mise gen:sqlc-server` to generate Go code

   ### Backend Implementation
   - Implement in `server/internal/<service>/impl.go`
   - Follow service struct pattern
   - Use context propagation, proper error handling
   - Add appropriate logging

   ### Frontend Implementation (if needed)
   - Add API integration via `@gram/client`
   - Use `@tanstack/react-query` for data fetching
   - Follow Moonshine design system
   - Add to appropriate page/component

   ### Background Jobs (if needed)
   - Add Temporal activities in `server/internal/background/`
   - Define workflows for async operations

4. **Implement incrementally**:
   - Build one component at a time
   - Write tests as you go
   - Verify each component before moving on

5. **Write tests**:
   - Unit tests for business logic
   - Integration tests for API endpoints
   - Component tests for React components
   - Use table-driven tests in Go

6. **Run validation**:
   ```bash
   # Go
   mise lint:server
   mise test:server

   # TypeScript
   mise lint:client
   pnpm test

   # Full build
   mise build:server
   ```

7. **Create PR** using `/pr` command:
   - Use appropriate prefix: `feat:`, `fix:`, `chore:`, `mig:`
   - Write clear description
   - Reference any related issues

## Task Tracking

Use this checklist format for tracking:

```
## Feature: [Name]

### Tasks
- [ ] Design API (if applicable)
- [ ] Database schema changes (if applicable)
- [ ] Generate code (Goa, SQLc)
- [ ] Backend implementation
- [ ] Frontend implementation (if applicable)
- [ ] Unit tests
- [ ] Integration tests
- [ ] Documentation
- [ ] Linting passes
- [ ] Manual testing
- [ ] PR created
```

## Arguments
- `$ARGUMENTS`: Feature description or requirements
