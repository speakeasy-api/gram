# Telemetry Package

This package handles telemetry data storage and retrieval using ClickHouse for high-performance analytics queries.

## ClickHouse Queries

Unlike PostgreSQL queries in other packages, ClickHouse queries are **not auto-generated** by sqlc since sqlc doesn't support ClickHouse. We use the **squirrel** query builder for dynamic query construction.

> **IMPORTANT:** Squirrel is ONLY used for ClickHouse queries in this package. All other database queries (PostgreSQL) MUST use sqlc-generated code. Do not use squirrel elsewhere in the codebase.

### Query Files

- **`queries.sql.go`**: Query implementations using squirrel query builder
- **`builder.go`**: Helper functions for ClickHouse-specific query patterns
- **`pagination.go`**: Cursor pagination helpers

### Adding a New Query

**Implement in `queries.sql.go`** using squirrel:

1. Create a params struct for the query inputs
2. Use `chSelect()` or `chInsert()` to build the query
3. Use helper functions from `builder.go` for optional filters
4. Use helper functions from `pagination.go` for cursor pagination

### Helper Functions

**builder.go** provides ClickHouse-specific helpers:
- `chSelect(columns...)` - Creates a SELECT builder with ClickHouse placeholder format
- `chInsert(table)` - Creates an INSERT builder
- `OptionalEq(sb, column, value)` - Adds WHERE only if value is non-empty
- `OptionalHas(sb, column, values)` - Adds array membership check
- `OptionalPosition(sb, column, value)` - Adds substring search
- `OptionalUUIDEq(sb, column, value)` - Adds UUID equality with `toUUIDOrNull()`
- `OptionalAttrEq(sb, attrExpr, value)` - Adds JSON attribute equality

**pagination.go** provides cursor pagination:
- `withPagination(sb, cursor, sortOrder)` - WHERE-based cursor for simple queries
- `withHavingPagination(sb, cursor, sortOrder, projectID, groupColumn, timeExpr)` - HAVING-based cursor for GROUP BY queries
- `withHavingTuplePagination(...)` - HAVING with tuple comparison for tie-breaking
- `withOrdering(sb, sortOrder, primaryCol, secondaryCol)` - ORDER BY helper

### Example Pattern

```go
type ListItemsParams struct {
    ProjectID    string
    DeploymentID string  // optional filter
    SortOrder    string
    Cursor       string
    Limit        int
}

func (q *Queries) ListItems(ctx context.Context, arg ListItemsParams) ([]Item, error) {
    sb := chSelect("id", "name", "created_at").
        From("items").
        Where("project_id = ?", arg.ProjectID)

    // Optional filters - no duplicate parameters needed!
    sb = OptionalEq(sb, "deployment_id", arg.DeploymentID)

    // Pagination
    sb = withPagination(sb, arg.Cursor, arg.SortOrder)
    sb = withOrdering(sb, arg.SortOrder, "created_at", "id")
    sb = sb.Limit(uint64(arg.Limit))

    query, args, err := sb.ToSql()
    if err != nil {
        return nil, fmt.Errorf("building query: %w", err)
    }

    rows, err := q.conn.Query(ctx, query, args...)
    // ... handle rows
}
```

## Pagination

### Service Layer Pagination (limit + 1 pattern)

Pagination logic lives in the **service layer** (`impl.go`), not the repo layer. The repo returns raw results, and the service handles cursor computation.

1. Client requests N items per page
2. Service queries repo with N+1 items
3. If N+1 items returned → compute `nextCursor` from item N, trim to N items
4. If ≤N items returned → `nextCursor = nil`, return all items
5. Cursor is the UUID of the last returned item

### Pagination Helpers

Use the helpers in `pagination.go`:

```go
// Simple queries (no GROUP BY) - uses WHERE clause
sb = withPagination(sb, arg.Cursor, arg.SortOrder)

// Aggregation queries (with GROUP BY) - uses HAVING clause
sb = withHavingPagination(sb, arg.Cursor, arg.SortOrder, arg.ProjectID, "trace_id", "min(time_unix_nano)")

// Aggregation with tie-breaking (when records may share the same timestamp)
sb = withHavingTuplePagination(sb, arg.Cursor, arg.SortOrder, arg.ProjectID, "chat_id", "min(time_unix_nano)")
```

### Optional Filters

Use helpers from `builder.go` instead of duplicating parameters:

```go
// Old pattern (raw SQL): (? = '' or deployment_id = ?) - required passing value twice
// New pattern (squirrel): conditional WHERE clause
sb = OptionalEq(sb, "deployment_id", arg.DeploymentID)  // only adds WHERE if non-empty
sb = OptionalHas(sb, "gram_urn", arg.GramURNs)          // array membership check
sb = OptionalPosition(sb, "gram_urn", arg.SearchTerm)   // substring search
```

## Testing

Tests use testcontainers to spin up a real ClickHouse instance. See `list_tool_logs_test.go` for examples.

Key testing patterns:
- Use `testenv.Launch()` in `TestMain` to set up infrastructure
- Create helper functions for inserting test data
- Use table-driven tests with descriptive names
- Add `time.Sleep(100 * time.Millisecond)` after inserts to allow ClickHouse to make data available

## Data Models

See `models.go` for struct definitions with ClickHouse field tags (`ch:"field_name"`).
