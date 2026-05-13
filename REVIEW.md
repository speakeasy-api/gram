# Code Review Guidelines

This document consolidates the project's coding conventions and review rules so automated reviewers (e.g. Devin Review) have full context without needing to traverse skill files.

## Project Structure

- `server/` — Go backend (Goa HTTP-RPC API, Temporal workflows)
- `client/dashboard/` — React frontend (TypeScript, Tailwind, Moonshine design system)
- `elements/` — React chat interface for Gram MCP servers
- `functions/` — Serverless function runner
- `cli/` — CLI for Gram
- `server/database/schema.sql` — DDL-only schema definition
- `server/migrations/` — Atlas-generated migration files (never hand-edit)
- `server/design/` — Goa API design files
- `server/gen/` — Generated code (DO NOT EDIT)
- `server/internal/*/repo/` — SQLc-generated code (DO NOT EDIT)

---

## Go (server/, functions/, cli/)

### General

- Go 1.25+ features are permitted.
- Prefer the standard library over third-party dependencies.
- Avoid editing files with a "DO NOT EDIT" comment.
- Leave NO todos, placeholders, or missing pieces.
- Avoid shallow one-line wrapper helpers that are only used once — inline them.

### Error Handling

- Use concise, unique `fmt.Errorf` wraps. No "failed to" prefix, no generic language:

```go
// Bad: "failed to save user: %w" or "run database query: %w"
// Good: "save user: %w"
return fmt.Errorf("save user: %w", err)
```

- In HTTP handlers, use the `oops` package for user-facing error mapping:

```go
return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
```

### Logging

- Always use `slog` context-aware methods: `DebugContext`, `InfoContext`, `WarnContext`, `ErrorContext`.
- Always include errors via `attr.SlogError(err)` from `server/internal/attr/conventions.go`.
- Never use bare `logger.Error(...)` without context.
- Use logging attributes from `server/internal/attr/conventions.go` — never ad-hoc string keys like `"user_id", userID`.
- Don't spam info-level logs. Focus on errors where appropriate.

### Dependency Injection

- Store dependencies on service structs via constructor-based injection.
- Do NOT hide dependencies in session manager state.
- Do NOT store `repo.Queries` on service structs for new services — inject `*pgxpool.Pool` and call `repo.New(s.db)` in handler methods.

### Third-Party Clients

- Constructors must always return a usable implementation (never nil).
- Provide a stub for local dev; choose real vs stub in `deps.go` based on environment.
- Do NOT expose vendor request/response types — define our own types at the boundary.

### Auth Context

- Assume `ActiveOrganisationID` is always present on `authctx`. Do NOT add defensive empty checks.

### Deferred Cleanup

- Never use bare `defer resource.Close()`.
- Use `o11y.LogDefer(ctx, logger, func() error { ... })` when the error matters.
- Use `o11y.NoLogDefer(func() error { ... })` when the error is inconsequential (tx rollbacks, resp body closes).

### Conversion Utilities

- Use `server/internal/conv` for pointer helpers, ternary expressions, and pgtype conversions. Do NOT reimplement inline.

### Struct Literals

- The `exhaustruct` linter requires all struct fields to be set. When adding fields to a type, update ALL call sites.

### Testing

- Use `require` from `github.com/stretchr/testify/require` exclusively for assertions.
- Use `t.Context()` instead of `context.Background()` (except inside `t.Cleanup` callbacks).
- Avoid `t.Run` subtests — prefer separate test functions.
- Never write bare SQL in tests. Use SQLc queries or service-level helpers.
- Use `testenv.NewLogger(t)`, `testenv.NewTracerProvider(t)`, `testenv.NewMeterProvider(t)` — not inline `slog.New(slog.DiscardHandler)`.
- Use `testify/mock` for mocking third-party integrations.

### Goa API Design

- Service/method names: camelCase. DSL types: PascalCase. Package names: lowercase no separators.
- Read methods: `GET`. Mutations: `POST`. Deletes with only id param: `DELETE`.
- Every method needs three OpenAPI meta keys: `operationId`, `x-speakeasy-name-override`, `x-speakeasy-react-hook`.
- Handlers never return `repo` types — always pass through `mv.Build<Subject>View(...)`.

### RBAC Enforcement

- Every mutating handler must call `s.authz.Require(ctx, authz.Check{...})` before database work.
- Use `RequireAny` only when a handler legitimately satisfies multiple equivalent scopes.
- Use `authz.Filter` for list endpoints — never a per-item `Require` loop.
- Adding a scope requires updates in 6+ places (see skill for checklist).

### Audit Logging

- Every mutation on a project/org-scoped resource must produce an audit entry per affected row.
- Audit writes go inside the same `dbtx` as the mutation — atomicity is non-negotiable.
- Cascading soft-deletes must emit per-row audit entries for each affected child.
- Treat audit-log failures as `oops.CodeUnexpected` — if it fails, fail the request.

---

## PostgreSQL / Database

### Schema Design

- All tables must have `project_id` (non-nullable, FK to `projects`).
- All tables must have `created_at` and `updated_at` columns with `clock_timestamp()` defaults.
- Prefer soft deletes with `deleted_at` + computed `deleted` column over `DELETE FROM`.
- All foreign key constraints must specify `ON DELETE SET NULL`.
- Use `snake_case` for identifiers, plural nouns for table names.
- Constraint naming: `{tablename}_{columnname(s)}_{suffix}` (key/fkey/idx/check/excl/seq).
- `server/database/schema.sql` is DDL only — no `DO`, `ALTER`, or procedural blocks.

### Backwards Compatibility

Never in a single migration:

- Add a non-nullable column to an existing table.
- Remove or rename a column.
- Change a column's data type or meaning.
- Add unique constraints without considering existing data.

Instead: add nullable columns, deprecate by making nullable, use expand-contract.

### SQLc Queries

- All queries live in `**/queries.sql` files.
- Every query MUST be scoped to a `project_id`.
- Use descriptive names.
- Never write bare SQL inline in application code — if a query doesn't exist, add it to SQLc.

### Migrations

- Migrations ship in their own PR. No app code alongside.
- Migration files and `atlas.sum` are produced only by `mise db:diff`. Never hand-edit.
- Follow expand-contract. Never drop a column in the same migration that adds others.
- Never run agents against dev or prod databases. Local only.

---

## ClickHouse (server/internal/telemetry/)

- Squirrel query builder is ONLY permitted for ClickHouse queries in the telemetry package.
- Do NOT use squirrel for PostgreSQL queries — those must use SQLc.
- Use pagination helpers from `pagination.go`.

---

## React Frontend (client/dashboard/, elements/)

### General

- Use `pnpm` package manager.
- Use `@gram/sdk` for server interactions.
- Use `@tanstack/react-query` for data fetching — never manual `useEffect`/`useState` for server state.
- When invalidating React Query caches, invalidate ALL relevant query keys (different hooks may use different prefixes).

### Component Structure

- Check `client/dashboard/src/components/` before writing any UI element. Reuse what exists.
- If the same Tailwind className appears on 3+ elements, extract to a component, `cva` variant, or named const.
- No copy-pasted JSX blocks — extract a parameterized component at 3 occurrences.
- No IIFEs in JSX. Extract to a named sub-component or variable.
- Components past ~150 lines of JSX are doing too much — break up.

### Performance Patterns

- Hoist `new RegExp()` into `useMemo` — never create inside render callbacks.
- Wrap search queries with `useDeferredValue` before expensive `useMemo` computations.
- Derive state during render, not via `useEffect` (prevents stale-value flash).
- Reset navigation state (currentIndex) when underlying data changes.

### Tooltip Usage

- `App.tsx` has a global `TooltipProvider`. NEVER add another `TooltipProvider` inside a component.
- Use `<Tooltip>`, `<TooltipTrigger>`, `<TooltipContent>` directly — they inherit the global provider.

### Styling

- ALWAYS use Moonshine design system utilities from `@speakeasy-api/moonshine`.
- NEVER use hardcoded Tailwind colors like `bg-neutral-100`, `border-gray-200`, `text-gray-500`.

### RBAC in the Dashboard

- Use `<RequireScope>` component for rendering gates (page/section/component levels).
- Use `useRBAC()` hook for imperative scope checks.
- The `Scope` type comes from `client/dashboard/src/pages/access/types.ts` — must stay in lockstep with server.

---

## Git & PR Conventions

- Commit messages: concise, focus on "why" not "what". Use conventional prefixes (feat, fix, refactor, docs, test, chore).
- Keep commits atomic — one logical change per commit.
- Migrations ship in their own PR, separate from app code.
- Changesets (`.changeset/<slug>.md`) required for server/dashboard/SDK changes.
