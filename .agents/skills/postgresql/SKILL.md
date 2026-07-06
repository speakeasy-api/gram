---
name: postgresql
description: Rules when working with PostgreSQL database in Gram
---

# PostgreSQL Best Practices

Comprehensive guidelines when working with PostgreSQL database to build Gram which include rules for schema design, database migration and application logic. All rules are kept in a rules folder with names of each rule outlined below (e.g. `rules/<rule-name>.md`).

## When to Apply

Reference these guidelines when:

- Creating database migrations
- Writing queries used in application code especially with SQLc
- Updating existing database schemas
- Creating pull requests that involve database changes

## Rules

- **Code Formatting and Comments:**
  - Maintain consistent code formatting using a tool like `pgformatter` or similar.
  - Use clear and concise comments to explain complex logic and intentions. Update comments regularly to avoid confusion.
  - Use inline comments sparingly; prefer block comments for detailed explanations.
  - Write comments in plain, easy-to-follow English.
  - Add a space after line comments (`-- a comment`); do not add a space for commented-out code (`--raise notice`).
  - Keep comments up-to-date; incorrect comments are worse than no comments.

- **Naming Conventions:**
  - Use `snake_case` for identifiers (e.g., `user_id`, `customer_name`).
  - Use plural nouns for table names (e.g., `customers`, `products`).
  - Use consistent naming conventions for functions, procedures, and triggers.
  - Choose descriptive and meaningful names for all database objects.

- **Data Integrity and Data Types:**
  - Use appropriate data types for columns to ensure data integrity (e.g., `INTEGER`, `VARCHAR`, `TIMESTAMP`).
  - Use constraints (e.g., `NOT NULL`, `UNIQUE`, `FOREIGN KEY`) to enforce data integrity.
  - Do not use `CHECK` constraints for pure enumeration / value validation (e.g. `CHECK (status IN ('active', 'inactive'))`). Validate allowed values in application code, where they can evolve without a migration. **Exception:** keep `CHECK`s that are structural to the Class Table Inheritance (CTI) pattern, e.g. pinning a subtype's discriminator (`CHECK (provider = 'aws_kms')`) so the composite foreign key back to the supertype enforces 1:1 semantics.
  - Define primary keys for all tables.
  - Use foreign keys to establish relationships between tables.
  - Utilize domains to enforce data type constraints reusable across multiple columns.
  - All foreign keys constraints must ALWAYS specify an `ON DELETE SET NULL` clause.

- **Indexing:**
  - Create indexes on columns frequently used in `WHERE` clauses and `JOIN` conditions.
  - Avoid over-indexing, as it can slow down write operations.
  - Consider using partial indexes for specific query patterns.
  - Use appropriate index types (e.g., `B-tree`, `Hash`, `GIN`, `GiST`) based on the data and query requirements.
  - When a unique key exists mainly to be a foreign-key target (e.g. a composite `(organization_id, id)` key a tenant-scoped child composite-FKs to for tenancy pinning), declare it as a `CREATE UNIQUE INDEX`, not a table-level `UNIQUE` constraint. Postgres accepts a non-partial, plain-column unique index as an FK target. This matters when adding the key to an existing table: `ALTER TABLE ... ADD CONSTRAINT ... UNIQUE` takes an `ACCESS EXCLUSIVE` lock and builds the index synchronously, whereas a `CREATE UNIQUE INDEX` (which Atlas automatically emits as `CONCURRENTLY` per its `concurrent_index` policy) does not. Trade-off: a concurrent index makes the migration non-transactional (Atlas emits `-- atlas:txmode none` for you), so keep such migrations minimal.

- **Schema evolution:**
  - Use expand-contract pattern instead of removing existing columns from a schema. Introduce new columns instead when appropriate.
  - ALWAYS call out when making a backwards incompatible schema change.
  - Suggest running `mise db:diff <migration-name>` after making schema changes to generate a migration file. Replace `<migration-name>` with a clear snake-case migration id such as `users-add-email-column`.
  - If you need to undo a migration then run: 1. `mise run db:reset` 2. `mise run db:migrate` to re-run all migrations from the beginning.

<relevant-tasks>

- `mise run db:diff <name-of-migrations>`: Create a database migration
- `mise run db:reset`: Drop the database and re-create it. No migrations applied at this point.
- `mise run db:migrate`: Run all pending database migrations. If you have just reset the database, this will run all migrations from the beginning.

</relevant-tasks>

## Schema design rules

### Multi-tenancy by project

When creating any tables, add a non-nullable column named `project_id` of type `uuid` with a foreign key constraint to the `projects` table. If appropriate to the nature and usage patterns of the table also include `organization_id TEXT NOT NULL` column.

### Change tracking

All tables should have `created_at` and `updated_at` columns:

```sql
create table if not exists example (
  -- ...
  created_at timestamptz not null default clock_timestamp(),
  updated_at timestamptz not null default clock_timestamp() on update clock_timestamp(),
  -- ...
);
```

### Always soft delete

A nullable `deleted_at` column may be added to tables to perform soft deletes:

```sql
create table if not exists example (
  -- ...
  deleted_at timestamptz,
  deleted boolean not null generated always as (deleted_at is not null) stored,
  -- ...
);
```

Deleting rows with `DELETE FROM table` is not strongly discouraged. Instead,
use:

```sql
UPDATE example SET deleted_at = clock_timestamp() WHERE id = ?;
```

### File structure

`server/database/schema.sql` is DDL only — no `DO`, `ALTER`, or other procedural blocks. Declare tables in dependency order so every `FOREIGN KEY` resolves inline; if a target is declared later, move it up.

### Constraint naming

All constraints should be named with this format:

```
{tablename}_{columnname(s)}_{suffix}
```

Where suffix is:

- `key` for a unique constraint
- `fkey` for a foreign key constraint
- `idx` for any other kind of index
- `check` for a check constraint
- `excl` for an exclusion constraint
- `seq` for an sequences

## Reviewing schema changes

### Backwards compatibity

Ensure that all schema changes are designed for backwards compatibility.

These are examples of terrible practices to avoid:

- Adding a non-nullable column to an existing table.
- Removing a column from a table.
- Changing the data type of an existing column.
- Renaming an existing column.
- Changing the meaning or usage of an existing column.
- Adding unique constraints or indexes to existing columns without considering the impact on existing data and queries.

Instead, strongly consider these better alternatives:

- Adding nullable columns to existing tables.
- Deprecating columns by making them nullable.
- Using expand-contract pattern for evolving schemas without causing outages.

## Database migrations

These rules apply any time you touch `server/migrations/`, `atlas.sum`, or `server/database/schema.sql`. They are non-negotiable.

Gram uses [Atlas](https://atlasgo.io) in versioned mode. Two file kinds are involved, and they are not the same thing:

- `server/database/schema.sql` is the **SDL** — the declarative, desired-state schema. This is the file you edit.
- `server/migrations/*.sql` are the **DDL** diff Atlas generates from that schema. Running `mise db:diff <name>` computes the delta (e.g. `ALTER TABLE ... ADD COLUMN ...`), writes a new timestamped migration file, and updates `atlas.sum`.

Rules:

- **Migrations ship in their own PR.** No application/business-logic code, no backfills, no unrelated changes alongside. Shipping migrations with business logic risks outages — the server can query a schema that has not rolled out yet — and makes the PR hard to revert.
- **Migration files and `atlas.sum` are produced only by the Atlas CLI (`mise db:diff`).** Never hand-edit, rename, or rehash them.
- **Migration files contain only DDL — never DML.** Backfills and other data manipulation (`INSERT` / `UPDATE` / `DELETE`) do not belong in a migration file. Data migrations live in application code, not migrations.
- **Follow expand-contract.** Never drop a column or table in the same migration that adds others. If a column is unwanted, mark it nullable with a comment and leave it for a later contract migration; sticking around for a few days is fine.
- **Never run agents (or any tooling) against dev or prod databases.** Local databases only.
- **Out-of-order timestamps:** if `mise lint:migrations` (or CI) reports a migration timestamp at or before the latest on `main`, do NOT rename the file. Delete the offending migration on your branch, rebase/merge `main`, then re-run `mise db:diff <name>` so the migration is regenerated on top with a fresh timestamp.
- **Migration merge conflicts:** never resolve them by hand. Delete your migrations, rebase/merge `main`, then re-run `mise db:diff` so your changes are recreated on top.

## Writing queries with SQLc

All SQLc queries live in `**/queries.sql` files in the codebase. This is an important convention to maintain.

When writing SQLc queries, follow these guidelines:

- Use descriptive names for queries and parameters.
- Write clear and efficient SQL queries that follow best practices for performance and readability.
- Consume the corresponding database schema to understand what tables, columns, relationships and indexes exist.
- CRITICAL: No matter the query, it MUST ALWAYS be scoped to a `project_id` to explicitly limit the scope of writes.

<relevant-tasks>

- `mise run infra:start`: bring up the local Postgres/ClickHouse/etc containers — required before running sqlc, since sqlc connects to the database to type-check queries.
- `mise run gen:sqlc-server`: generates Go code from SQLc queries (requires the local database from `mise run infra:start`).

</relevant-tasks>
