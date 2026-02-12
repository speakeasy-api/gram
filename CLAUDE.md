# Gram Project Structure Guide

This document provides an overview of the key directories in the Gram project to help you understand the codebase organization.

<tip>
If you've just cloned this repository, then consider running `./zero --agent` to get your development environment set up.
</tip>

## Key Directories

<structure>

- `/`: Root directory of the Gram project
  - `mise.toml`: Default environment variables are configured here and support running Gram and its tasks.
  - `mise.local.toml`: Local environment variable overrides for development. This file is ignored by git and should not be committed.
  - `.mise-tasks/**/*.{mts,sh}`: Useful tasks for working with the project
  - `go.mod`: Go module definition for the entire project
  - `server/`: Main backend service codebase
  - `cli/`: Command-line interface for Gram that users use to interact with the Gram service
  - `functions/`: Serverless function runner powering the Gram Functions feature
  - `ts-framework/functions/`: TypeScript SDK for function authors (`Gram.tool()` API, manifest generation, MCP passthrough)
  - `client/`: Frontend React application for Gram
  - `elements/`: Frontend React application for Gram Elements, a chat interface that integrates with Gram MCP servers.

</structure>

### server

Contains the main application code for the Gram server:

<structure>

- `internal/`: The implementation of the server logic.
  - `background/`: Temporal workflows and activities are implemented here.
  - `conv/`: Useful conversion functions for converting between different Go types.
  - `mv/`: Re-usable model views for representing Gram API resources.
  - `oops/`: Error handling utilities to be used across Gram service implementation files.
  - `openapi/`: OpenAPI parsing package used to generate tools as part of the Gram deployments service.
  - `testenv/`: Utilities for setting up test environments that support writing tests.
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

</commands>

### Atlas Migration Troubleshooting

- **"migration file X was added out of order" error**: Rename the migration file to have a timestamp after the latest existing migration (e.g., `20260129_foo.sql` → `20260203000001_foo.sql`), then run `mise db:hash` to regenerate `atlas.sum`.

## Mise CLI

The `mise` tasks listed in this guide should be used where building, testing or linting is needed. The commands can take arguments directly and don't need a `--` separator. For example, to run the server in development mode, use:

```
mise start:server --dev-single-process
```

<important>

- Run `mise tasks` to discover available tasks.
- Run `mise run <task-name> --help` to get help for a specific task including any arguments it takes.

</important>

## Skills

Skills provide domain-specific rules and best practices.

<important>

Activate a skill when your task falls within its scope.

</important>

| Skill                         | When to activate                                                           |
| ----------------------------- | -------------------------------------------------------------------------- |
| `golang`                      | Writing or editing Go code                                                 |
| `postgresql`                  | Creating migrations, writing SQLc queries, or changing the database schema |
| `clickhouse`                  | Working with ClickHouse queries in the `server/` package                   |
| `frontend`                    | Working on the React frontends in `client/` or `elements/`                 |
| `vercel-react-best-practices` | Optimizing React performance, reviewing components for best practices      |
| `gram-functions`              | Understanding or modifying the Gram Functions serverless execution feature |
| `mise-tasks`                  | Creating or editing mise task scripts in `.mise-tasks/`                    |
