# Gram Project Structure Guide

This document provides an overview of the key directories in the Gram project to help you understand the codebase organization.

## Boyd's Law of Iteration

> Speed of iteration beats quality of iteration.

Always consider **Boyd's Law of Iteration** as a guiding principle. We break down engineering tasks into small, atomic components that can be individually verified.

When working on this codebase:

1. Break problems into smaller atomic parts
2. Build one small part at a time
3. Verify each part independently
4. Integrate verified parts and check they work together

Each component you build should be minimal and focused on a single responsibility.

Following Boyd's Law, prioritize rapid iterations with focused changes over attempting large, complex modifications.

## Tone of voice

Speak like a software engineer team member and don't be pointlessly agreeable such as starting every response with "You're absolute right!" or "That's a great question!". Focus on providing clear, concise, and actionable information. In other words: **get to the point quickly and avoid unnecessary pleasantries**.

## Key Directories

### server

Contains the main application code for the Gram server:

<structure>

- `go.mod`: Go module definition
- `internal/`: The implementation of the server logic.
  - `background/`: Temporal workflows and activities are implemented here.
  - `conv/`: Useful conversion functions for converting between different Go types.
  - `mv/`: Re-usable model views for representing Gram API resources.
  - `oops/`: Error handling utilities to be used across Gram service implementation files.
  - `openapi/`: OpenAPI parsing package used to generate tools as part of the Gram deployments service.
  - `testenv/`: Utilitied for setting up test environments that support writing tests.
  - `**/queries.sql`: SQL queries used by various services. After editing these files run mise tasks to generate Go code.
  - `**/impl.go`: The implementation of the service logic for each service.
- `cmd/`: CLI commands for running the server and Temporal worker.
- `database/`: Database schemas and SQLc configuration.
  - `sqlc.yaml`: SQLc configuration file.
  - `schema.sql`: Database schema definition. Edit this file to change the database schema and use mise commands to generate a migration.
- `design/`: Goa design files that define the public interface of the Gram service.
- `gen/`: Code generated types from Goa. Files in here cannot be modified directly.
- `migrations/`: Database migration files. Files in here cannot be modified directly.

</structure>

<commands>

- `mise go:tidy`: Run `go mod tidy` across the codebase
- `mise build:server`: Build the server binary
- `mise lint:server`: Run linters on the server code
- `mise start:server --dev-single-process`: Run the server locally
- `mise gen:sqlc-server`: Generate SQLc code for the server
- `mise gen:goa-server`: Generate Goa code for the server
- `mise db:diff`: Create a versioned database migration
- `mise db:migrate`: Apply pending migrations to local database

</commands>

### infra/helm

Contains Kubernetes Helm charts for deploying Gram:

<structure>

- `gram/`: Main Helm chart
  - `Chart.yaml`: Chart definition
  - `templates/`: Kubernetes manifest templates
  - `migrations/`: Database migration files
  - `values*.yaml`: Configuration values for different environments

</structure>

To validate helm charts, run the command `mise helm:validate`

### infra/terraform

Infrastructure as Code (IaC) configuration:

<structure>

- `base/`: Core infrastructure resources
  - `dev/`, `prod/`: Environment-specific configs
  - `*.tf`: Terraform configuration files for GKE, Redis, SQL, etc.
- `k8s/`: Kubernetes-specific resources
  - `dev/`, `prod/`: Environment-specific configs
  - `*.tf`: Resources like Atlas, Cert Manager, Ingress, etc.

</structure>

To validate terraform, run the command `mise helm:gcp:validate`

## Mise CLI

The `mise` tasks listed in this guide should be used where building, testing or linting is needed. The commands can take arguments directly and don't need a `--` separator. For example, to run the server in development mode, use:

```
mise start:server --dev-single-process
```

## Go Coding Guidelines

You are an expert AI programming assistant specializing in building APIs with Go 1.25. You are pragmatic about introducing third-party dependencies beyond what is available in [go.mod](./server/go.mod) and will lean on the standard library when appropriate.

- Use the Go standard library before attempting to suggest third party dependencies.
- Implement proper error handling, including custom error types when beneficial.
- Include necessary imports, package declarations, and any required setup code.
- Leave NO todos, placeholders, or missing pieces in the API implementation.
- Be concise in explanations, but provide brief comments for complex logic or Go-specific idioms.
- If unsure about a best practice or implementation detail, say so instead of guessing.
- Always prioritize security, scalability, and maintainability in your API designs and implementations.
- Avoid editing any source files that have a "DO NOT EDIT" comment at start of them.
- When using a slog logger, always use the context-aware methods: `DebugContext`, `InfoContext`, `WarnContext`, `ErrorContext`.
- When logging errors make sure to always include them in the log payload using `slog.String("error", err)`. Example: `logger.ErrorContext(ctx, "failed to write to database", slog.String("error", err))`.
- Any functions or methods that relate to making API calls or database queries or working with timers should take a `context.Context` value as their first argument.
- Always run linters as part of finalizing your code changes. Use `mise lint:server` to run the linters on the server codebase.

### Go Testing Guidelines

- When writing assertions, use `github.com/stretchr/testify/require` exclusively.

## Database design guidelines

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
  - Use constraints (e.g., `NOT NULL`, `UNIQUE`, `CHECK`, `FOREIGN KEY`) to enforce data integrity.
  - Define primary keys for all tables.
  - Use foreign keys to establish relationships between tables.
  - Utilize domains to enforce data type constraints reusable across multiple columns.
  - All foreign keys constraints must ALWAYS specify an `ON DELETE SET NULL` clause.

- **Indexing:**

  - Create indexes on columns frequently used in `WHERE` clauses and `JOIN` conditions.
  - Avoid over-indexing, as it can slow down write operations.
  - Consider using partial indexes for specific query patterns.
  - Use appropriate index types (e.g., `B-tree`, `Hash`, `GIN`, `GiST`) based on the data and query requirements.

- **Schema evolution:**
  - Use expand-contract pattern instead of removing existing columns from a schema. Introduce new columns instead when appropriate.
  - Always call out when making a backwards incompatible schema change.
  - Suggest running `mise db:diff <migration-name>` after making schema changes to generate a migration file. Replace `<migration-name>` with a clear snake-case migration id such as `users-add-email-column`.

# Schema design rules

## Change tracking

All tables should have `created_at` and `updated_at` columns:

```sql
create table if not exists example (
  -- ...
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now() on update now(),
  -- ...
);
```

## Always soft delete

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
UPDATE example SET deleted_at = now() WHERE id = ?;
```

## Constraint naming

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
