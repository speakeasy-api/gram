# Telemetry Package

This package handles telemetry data storage and retrieval using ClickHouse for high-performance analytics queries.

## ClickHouse Queries

Unlike PostgreSQL queries in other packages, ClickHouse queries are **not auto-generated** by sqlc since sqlc doesn't support ClickHouse. However, we follow sqlc conventions for consistency.

### Query Files

- **`queries.sql`**: Human-readable SQL queries following sqlc conventions with `-- name:` comments
- **`queries.sql.go`**: Manual Go implementations of the queries

### Adding a New Query

1. **Add the query to `queries.sql`** with sqlc-style formatting:
   ```sql
   -- name: GetSomething :one
   select * from table where id = ?;
   ```

2. **Implement the function in `queries.sql.go`**:
   - Extract the query as a const with the sqlc comment
   - Create a params struct (if needed) right before the function
   - Implement the function following existing patterns

3. **Ask Claude Code to generate the implementation** - it will follow the sqlc conventions established in this package.

### Example Pattern

**In `queries.sql`:**
```sql
-- name: ListItems :many
select id, name from items where project_id = ? limit ?;
```

**In `queries.sql.go`:**
```go
const listItems = `-- name: ListItems :many
select id, name from items where project_id = ? limit ?
`

type ListItemsParams struct {
    ProjectID string
    Limit     int
}

func (q *Queries) ListItems(ctx context.Context, arg ListItemsParams) ([]Item, error) {
    rows, err := q.conn.Query(ctx, listItems, arg.ProjectID, arg.Limit)
    // ... implementation
}
```

## Pagination

This package uses cursor-based pagination with the "limit + 1" pattern:

1. Client requests N items per page
2. Query fetches N+1 items
3. If N+1 items returned → `hasNextPage = true`, return only N items
4. If ≤N items returned → `hasNextPage = false`, return all items
5. Cursor is the ID of the last returned item

### ClickHouse-Specific Patterns

#### Nil UUID Sentinel
ClickHouse doesn't support short-circuit evaluation in OR expressions, so we use the nil UUID (`00000000-0000-0000-0000-000000000000`) as a sentinel value to indicate "no cursor" (first page):

```sql
and if(
    toUUID(?) = toUUID('00000000-0000-0000-0000-000000000000'),
    true,
    -- cursor comparison logic
)
```

#### Optional Filters
Use the pattern `(? = '' or field = ?)` to make filters optional:

```sql
and (? = '' or deployment_id = ?)
```

Pass empty string when filter is not needed, or pass the value twice when filtering:
```go
arg.DeploymentID, arg.DeploymentID  // pass twice for the pattern above
```

#### UUIDv7 Timestamp Extraction
Use `UUIDv7ToDateTime(toUUID(?))` to extract the embedded timestamp from UUIDv7 for cursor-based pagination:

```sql
timestamp > UUIDv7ToDateTime(toUUID(?))
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
