---
cwd: ../..
shell: bash
---

# Writing queries with SQLC

We use [SQLC](https://sqlc.dev/) to generate type-safe Go code from SQL queries. SQLC allows us to write SQL queries and automatically generates Go structs and functions, eliminating the need for manual mapping between database rows and Go structs.
We also use [pgx v5](https://github.com/jackc/pgx)'s [pgxpool](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool) to connect to the database and execute queries.

The overall process is:

1. Write SQL queries in service-specific `queries.sql` files
2. Generate Go code using SQLC
3. Use the generated repository in your service implementation

## Step 1. Understanding the SQLC setup

SQLC is configured in [`server/database/sqlc.yaml`](../../server/database/sqlc.yaml). Each service has its own repository generated from its `queries.sql` file:

- `server/internal/projects/queries.sql` → `server/internal/projects/repo/`
- `server/internal/tools/queries.sql` → `server/internal/tools/repo/`
- `server/internal/environments/queries.sql` → `server/internal/environments/repo/`
- And so on...

Each repository generates:

- `db.go` - Database interface and transaction support
- `models.go` - Go structs for database tables
- `*.sql.go` - Generated functions for each query

## Step 2. Writing SQL queries

Create or edit the `queries.sql` file in your service directory (e.g., `server/internal/{your-service}/queries.sql`).

### Query naming conventions

Use descriptive names that indicate the operation and return type:

```sql
-- name: CreateUser :one
INSERT INTO users (name, email) VALUES (@name, @email) RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = @id AND deleted IS FALSE;

-- name: ListUsersByOrganization :many
SELECT * FROM users
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: UpdateUserEmail :one
UPDATE users
SET email = @email, updated_at = clock_timestamp()
WHERE id = @id AND deleted IS FALSE
RETURNING *;

-- name: DeleteUser :exec
UPDATE users
SET deleted_at = clock_timestamp()
WHERE id = @id AND deleted IS FALSE;
```

### Query annotations

- `:one` - Returns a single row (or `sql.ErrNoRows` if not found)
- `:many` - Returns multiple rows as a slice
- `:exec` - Executes the query without returning data
- `:execrows` - Executes and returns the number of affected rows

See more [here](https://docs.sqlc.dev/en/stable/reference/query-annotations.html)

### Parameter naming

Use `@parameter_name` for named parameters:

```sql
-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = @email;

-- name: CreateProject :one
INSERT INTO projects (name, slug, organization_id)
VALUES (@name, @slug, @organization_id)
RETURNING *;
```

### Optional parameters

Use `sqlc.narg()` for optional parameters:

```sql
-- name: ListProjects :many
SELECT * FROM projects
WHERE organization_id = @organization_id
  AND (sqlc.narg(cursor)::uuid IS NULL OR id < sqlc.narg(cursor))
  AND deleted IS FALSE
ORDER BY id DESC
LIMIT $1;
```

### Complex queries with CTEs

For complex queries, use Common Table Expressions (CTEs):

```sql
-- name: GetProjectWithTools :one
WITH latest_deployment AS (
    SELECT d.id
    FROM deployments d
    JOIN deployment_statuses ds ON d.id = ds.deployment_id
    WHERE d.project_id = @project_id
      AND ds.status = 'completed'
    ORDER BY seq DESC
    LIMIT 1
)
SELECT
    p.*,
    COUNT(htd.id) as tool_count
FROM projects p
LEFT JOIN http_tool_definitions htd ON htd.deployment_id = (SELECT id FROM latest_deployment)
WHERE p.id = @project_id
  AND p.deleted IS FALSE
GROUP BY p.id;
```

## Step 3. Generate SQLC code

After writing your queries, generate the Go code:

```bash
mise run gen:sqlc-server
```

This will update the generated repository code in your service's `repo/` directory.

## Step 4. Use the repository in your service

In your service implementation, inject and use the generated repository:

```go
package yourservice

import (
    "context"
    "github.com/speakeasy-api/gram/server/internal/yourservice/repo"
)

type Service struct {
    db   *pgxpool.Pool
    repo *repo.Queries
}

func NewService(db *pgxpool.Pool) *Service {
    return &Service{
        db:   db,
        repo: repo.New(db),
    }
}

func (s *Service) CreateUser(ctx context.Context, name, email string) (*repo.User, error) {
    return s.repo.CreateUser(ctx, repo.CreateUserParams{
        Name:  name,
        Email: email,
    })
}

func (s *Service) GetUser(ctx context.Context, id uuid.UUID) (*repo.User, error) {
    user, err := s.repo.GetUserByID(ctx, id)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, oops.C(oops.CodeNotFound)
        }
        return nil, oops.E(oops.CodeUnexpected, err, "failed to get user")
    }
    return user, nil
}
```

## Step 5. Handle database transactions

For operations that need to be atomic, use database transactions:

```go
func (s *Service) CreateUserWithProfile(ctx context.Context, name, email string) (*repo.User, error) {
    // Begin a transaction
    dbtx, err := s.db.Begin(ctx)
    if err != nil {
        return nil, oops.E(oops.CodeUnexpected, err, "failed to begin transaction")
    }
    defer dbtx.Rollback(ctx)

    // Create a repo with the transaction
    tx := s.repo.WithTx(dbtx)

    // Create user
    user, err := tx.CreateUser(ctx, repo.CreateUserParams{
        Name:  name,
        Email: email,
    })
    if err != nil {
        return nil, oops.E(oops.CodeUnexpected, err, "failed to create user")
    }

    // Create profile
    _, err = tx.CreateProfile(ctx, repo.CreateProfileParams{
        UserID: user.ID,
        Bio:    "New user",
    })
    if err != nil {
        return nil, oops.E(oops.CodeUnexpected, err, "failed to create profile")
    }

    // Commit transaction
    if err := dbtx.Commit(ctx); err != nil {
        return nil, oops.E(oops.CodeUnexpected, err, "failed to commit transaction")
    }

    return user, nil
}
```

## Step 6. Handle errors properly

Always handle SQLC errors appropriately:

```go
func (s *Service) GetUser(ctx context.Context, id uuid.UUID) (*repo.User, error) {
    user, err := s.repo.GetUserByID(ctx, id)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, oops.C(oops.CodeNotFound, "user not found")
        }
        return nil, oops.E(oops.CodeUnexpected, err, "failed to get user")
    }
    return user, nil
}
```

## Step 7. Test your queries

Create tests for your repository functions:

```go
func TestCreateUser(t *testing.T) {
    // Setup test database
    db := testenv.SetupDB(t)
    repo := repotest.New(db)

    // Test the query
    user, err := repo.CreateUser(context.Background(), repotest.CreateUserParams{
        Name:  "John Doe",
        Email: "john@example.com",
    })

    require.NoError(t, err)
    assert.Equal(t, "John Doe", user.Name)
    assert.Equal(t, "john@example.com", user.Email)
    assert.NotZero(t, user.ID)
    assert.NotZero(t, user.CreatedAt)
}
```

## Step 8. Database schema considerations

When writing queries, follow the established database patterns:

### Soft deletes

Always check for soft deletes:

```sql
-- name: GetUserByID :one
SELECT * FROM users
WHERE id = @id
  AND deleted IS FALSE;
```

### Timestamps

Use `clock_timestamp()` for updates

```sql
-- name: UpdateUser :one
UPDATE users
SET name = @name, updated_at = clock_timestamp()
WHERE id = @id AND deleted IS FALSE
RETURNING *;
```

### UUIDs

Use the `generate_uuidv7()` function for new records:

```sql
-- name: CreateUser :one
INSERT INTO users (id, name, email)
VALUES (generate_uuidv7(), @name, @email)
RETURNING *;
```

## Common patterns

### Pagination (todo: we don't have a lot of examples yet, so I made this up)

```sql
-- name: ListUsers :many
SELECT * FROM users
WHERE organization_id = @organization_id
  AND (sqlc.narg(cursor)::uuid IS NULL OR id < sqlc.narg(cursor))
  AND deleted IS FALSE
ORDER BY id DESC
OFFSET $1
LIMIT $2;
```

### Joins with aggregations

```sql
-- name: GetProjectStats :one
SELECT
    p.*,
    COUNT(htd.id) as tool_count,
    COUNT(d.id) as deployment_count
FROM projects p
LEFT JOIN deployments d ON d.project_id = p.id AND d.deleted IS FALSE
LEFT JOIN http_tool_definitions htd ON htd.deployment_id = d.id AND htd.deleted IS FALSE
WHERE p.id = @project_id
  AND p.deleted IS FALSE
GROUP BY p.id;
```

## Best practices

1. **Query naming**: Use descriptive names that indicate the operation and return type
2. **Parameter naming**: Use clear, descriptive parameter names with `@` prefix
3. **Error handling**: Always handle `sql.ErrNoRows` appropriately
4. **Transactions**: Use transactions for operations that need to be atomic
5. **Soft deletes**: Always check `deleted IS FALSE` in queries
6. **Timestamps**: Use `clock_timestamp()` over `now()` or `CURRENT_TIMESTAMP` for updates because it returns the actual current time at the point of execution of the SQL query, and its value changes within a transaction

## Troubleshooting

### Common issues

1. **"column does not exist"**: Check that your query matches the current schema, and you have run the migrations using `./zero`
2. **Transaction deadlocks**: Use proper transaction ordering and timeouts
3. **Type conversion**: Use the conv package to convert from Go types to SQLC types 

### Regenerating code

If you make changes to the schema or queries:

```bash
# Regenerate all SQLC code
mise run gen:sqlc-server
```

> [!TIP]
>
> You can run `mise run gen:server` to regenerate all server code (both SQLC and Goa) at once.

> [!WARNING]
>
> Never edit files in the `repo/` directories directly - they are generated and will be overwritten. Always make changes in the `queries.sql` files instead.
