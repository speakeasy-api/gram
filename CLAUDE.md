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
  - `mprocs.yaml`: Process manager config for `madprocs` — runs all local services (mock-idp, server, worker, dashboard, elements) in a single terminal with a tabbed UI. Run `madprocs` to start the TUI, or use `madprocs status|logs|start|stop|restart <proc>` from the CLI. Use `/madprocs` slash command for agent-assisted process control.
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

### client/dashboard

The main frontend application lives in `client/dashboard/` (not `client/` directly).

<commands>

- `cd client/dashboard && npx tsc --noEmit`: Type-check the dashboard
- `cd client/dashboard && pnpm build`: Build the dashboard
- `cd client/dashboard && pnpm dev`: Run dev server

</commands>

### Database Migrations

<important>

These rules apply any time you touch `server/migrations/`, `atlas.sum`, or `server/database/schema.sql`. They are non-negotiable.

</important>

- **Migrations ship in their own PR.** No app code, no backfills, no unrelated changes alongside.
- **Migration files and `atlas.sum` are produced only by the Atlas CLI.** Never hand-edit, rename, or rehash them. They must contain only DDL emitted by `mise db:diff`.
- **Follow expand-contract.** Never drop a column or table in the same migration that adds others. If a column is unwanted, mark it nullable with a comment and leave it for a later contract migration; sticking around for a few days is fine.
- **Never run agents (or any tooling) against dev or prod databases.** Local databases only.
- **Out-of-order timestamps:** if `mise lint:migrations` (or CI) reports a migration timestamp at or before the latest on `main`, do NOT rename the file. Delete the offending migration on your branch, rebase/merge `main`, then re-run `mise db:diff <name>` so the migration is regenerated on top with a fresh timestamp.
- **Migration merge conflicts:** never resolve them by hand. Delete your migrations, rebase/merge `main`, then re-run `mise db:diff` so your changes are recreated on top.

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
| `gram-management-api`         | Designing or modifying management API endpoints (Goa design, impl)         |
| `gram-audit-logging`          | Recording or exposing audit events via the auditlogs management API        |
| `gram-rbac`                   | Adding or enforcing authorization scopes, grants, or roles                 |
| `glint`                       | Authoring or editing analyzers in the `glint/` go/analysis package         |
| `mise-tasks`                  | Creating or editing mise task scripts in `.mise-tasks/`                    |
| `jaeger`                      | Testing backend endpoints locally and inspecting traces via Jaeger API     |
| `datadog`                     | Investigating errors, performance, incidents, or telemetry via Datadog     |
| `datadog-insights`            | Running the full Gram production health digest and posting it to Slack     |
| `madprocs`                    | Controlling local dev processes via mprocs (start, stop, restart, logs)    |
| `pr`                          | Creating a Pull Request for current changes                                |
| `spec`                        | Interviewing user in-depth to produce a detailed spec before building      |

# Plan Mode

- Make the plan extremely concise. Sacrifice grammar for the sake of concision.
- Identify any of the skills above that are relevant to the task so you can activate when implementing.
- At the end of each plan, give me a list of unresolved questions to answer, if any.
