# Telemetry Package

This package handles telemetry data storage and retrieval using ClickHouse for high-performance analytics queries.

## ClickHouse Queries

Unlike PostgreSQL queries in other packages, ClickHouse queries are **not auto-generated** by sqlc since sqlc doesn't support ClickHouse. However, we follow sqlc conventions for consistency.

### Query Files

- **`queries.sql.go`**: Manual Go implementations of queries following sqlc patterns

**Note:** We do NOT maintain a separate `queries.sql` file. All queries are defined directly in `queries.sql.go`.

### Adding a New Query

**Implement directly in `queries.sql.go`**:

1. Define the query as a const with sqlc-style comment header
2. Create a params struct (if needed) right before the function
3. Implement the function following existing patterns

### Example Pattern

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
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var items []Item
    for rows.Next() {
        var i Item
        if err = rows.ScanStruct(&i); err != nil {
            return nil, fmt.Errorf("error scanning row: %w", err)
        }
        items = append(items, i)
    }

    return items, rows.Err()
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

### ClickHouse-Specific Patterns

#### Empty String Cursor Sentinel
Use empty string to indicate "no cursor" (first page). This avoids complex nil UUID checks:

```sql
AND (
    ? = '' OR
    IF(
        ? = 'asc',
        (time_unix_nano, toUUID(id)) > (SELECT time_unix_nano, toUUID(id) FROM table WHERE id = ? LIMIT 1),
        (time_unix_nano, toUUID(id)) < (SELECT time_unix_nano, toUUID(id) FROM table WHERE id = ? LIMIT 1)
    )
)
```

When calling from Go:
```go
cursor := ""  // First page
cursor := "some-uuid"  // Subsequent pages
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
