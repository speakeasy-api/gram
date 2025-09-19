# CRUSH.md - Gram Development Guide

## Build/Test/Lint Commands
- `mise start:server --dev-single-process` - Run server locally in single process mode
- `mise start:dashboard` - Start React dashboard dev server
- `mise test:server` - Run all Go tests
- `mise test:server ./internal/pkg/...` - Run tests for specific package
- `mise test:server -run TestFunctionName` - Run single test function
- `mise lint:server` - Run golangci-lint on Go code
- `mise lint:client` - Run ESLint on TypeScript/React code
- `mise build:server` - Build Go server binary
- `mise gen:sqlc-server` - Generate Go code from SQL queries
- `mise gen:goa-server` - Generate Goa API code
- `mise db:migrate` - Apply database migrations
- `mise db:diff migration-name` - Create new migration after schema changes

## Go Code Style
- Use `snake_case` for database identifiers, `camelCase` for Go
- Always use context-aware slog methods: `logger.InfoContext(ctx, "msg", slog.String("error", err))`
- Functions making API/DB calls must take `context.Context` as first parameter
- Use `github.com/stretchr/testify/require` for test assertions
- Follow standard library over third-party deps when possible
- Never edit files with "DO NOT EDIT" comments
- Update `queries.sql` files, not raw SQL - run `mise gen:sqlc-server` after changes

## Database Design
- Use `snake_case` for all identifiers, plural table names
- All tables need `created_at`, `updated_at` timestamptz columns
- Soft delete with `deleted_at` timestamptz, `deleted` generated boolean
- Foreign keys must have `ON DELETE SET NULL`
- Constraint naming: `{table}_{column}_{suffix}` (key/fkey/idx/check/excl/seq)

## Frontend (React/TypeScript)
- Use Radix UI components and Tailwind CSS
- Follow existing component patterns in `client/dashboard/src/components/`
- Use React Query for API calls via generated SDK hooks