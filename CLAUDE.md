# Gram Project Structure Guide

This document provides an overview of the key directories in the Gram project to help you understand the codebase organization.

<tip>
If you've just cloned this repository, then consider running `./zero --agent` to get your development environment set up.
</tip>

## Customer Data Is Confidential

<important>

Never include customer-identifying information in anything that gets committed or pushed: customer names, organization ids, project ids, external account ids, emails, or revenue/spend figures. This applies to EVERY surface that leaves the machine — branch names, commit messages, PR titles and bodies, file contents, file NAMES, changesets, test fixtures, and code comments. This repository is public, and pushed data propagates to surfaces that cannot be scrubbed afterwards: pull request refs outlive branch deletion, CI logs embed PR titles, and review bots ingest full diffs.

- Use placeholders (`<PROJECT_ID>`, `<ORG_ID>`, "the customer") in runbooks, migrations, docs, and tests — even when hardcoding a real value would be more convenient. Keep concrete values in internal systems (e.g. Linear) and fill them in at execution time.
- Ticket ids (e.g. ABC-123) are fine; ticket URLs whose slugs embed a customer name are not.
- Before committing or pushing, check every surface: branch name, commit message, changed file contents and names, changeset text, PR title/body.

</important>

## Key Directories

<structure>

- `/`: Root directory of the Gram project
  - `mise.toml`: Default environment variables are configured here and support running Gram and its tasks.
  - `mise.local.toml`: Local environment variable overrides for development. This file is ignored by git and should not be committed.
  - `.mise-tasks/**/*.{mts,sh}`: Useful tasks for working with the project
  - `go.mod`: Go module definition for the entire project
  - `pitchfork.toml`: Process manager config for `pitchfork` — runs all local services (mock-idp, server, worker, dashboard) in a single terminal with a tabbed UI. Use `pitchfork list|status|logs|start|stop|restart <daemon>` from the CLI.
  - `server/`: Main backend service codebase
  - `cli/`: Command-line interface for Gram that users use to interact with the Gram service
  - `functions/`: Serverless function runner powering the Gram Functions feature
  - `ts-framework/functions/`: TypeScript SDK for function authors (`Gram.tool()` API, manifest generation, MCP passthrough)
  - `client/`: Frontend React application for Gram. Gram Elements — a chat interface that integrates with Gram MCP servers — lives inside it at `client/dashboard/src/elements/`.

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
- `mise build:tunnel-gateway`: Build the tunnel gateway binary
- `mise lint:server`: Run linters on the server code
- `mise run start`: Run the process manager that spins up local servers (server, worker, idp, ...)
- `hk fix`: Runs formatters across changed files in the current branch.

</commands>

### client/dashboard

The main frontend application lives in `client/dashboard/` (not `client/` directly).

<commands>

- `pnpm -F dashboard type-check`: Type-check the dashboard
- `pnpm -F dashboard build`: Build the dashboard
- `pnpm -F dashboard dev`: Run dev server

</commands>

### Dashboard browser automation

Use the `gram-playwright-cli` skill and `mise run playwright` for routine dashboard inspection, page interaction, console or network debugging, and screenshots. The mise task uses the repository Playwright config, installs Chromium when missing, and writes ignored artifacts to `.playwright-cli/`.

Use `pr-demo-gif` when a user-visible change needs a shareable PR screenshot, GIF recording, or PR comment. It builds on the same `mise run playwright` workflow and adds the capture and publishing steps. Do not use `npm`, `npx`, or `yarn` for either workflow.

### Testing assistants locally

`.mcp.json` registers the `assistants-dev` MCP server (`server/cmd/dev-mcp`), which drives the local management API without the dashboard UI. It logs into the local stack on its own (dev-idp auto-approves), so no setup is needed beyond a running dev stack. Use its tools — assistant CRUD, `run_turn` (send a message and wait for the assistant's reply), `load_chat`, and trigger CRUD — to exercise assistant runtime changes end to end. `whoami` lists the available project slugs.

### Database Migrations

Migration rules live in the `postgresql` skill (`.agents/skills/postgresql/SKILL.md`, "Database migrations" section). Activate that skill any time you touch `server/migrations/`, `atlas.sum`, or `server/database/schema.sql`.

## Mise CLI

The `mise` tasks listed in this guide should be used where building, testing or linting is needed. The commands can take arguments directly and don't need a `--` separator. For example, to run the server in development mode, use:

```
mise run start
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

| Skill                             | When to activate                                                                |
| --------------------------------- | ------------------------------------------------------------------------------- |
| `golang`                          | Writing or editing Go code                                                      |
| `postgresql`                      | Creating migrations, writing SQLc queries, or changing the database schema      |
| `clickhouse`                      | Working with ClickHouse queries, schema, or migrations in the `server/` package |
| `frontend`                        | Working on the React frontend in `client/`                                      |
| `vercel-react-best-practices`     | Optimizing React performance, reviewing components for best practices           |
| `gram-functions`                  | Understanding or modifying the Gram Functions serverless execution feature      |
| `gram-management-api`             | Designing or modifying management API endpoints (Goa design, impl)              |
| `gram-audit-logging`              | Recording or exposing audit events via the auditlogs management API             |
| `gram-rbac`                       | Adding or enforcing authorization scopes, grants, or roles                      |
| `gram-pubsub`                     | Declaring Pub/Sub topics/subscriptions via proto, or publishing/consuming       |
| `gram-pubsub-python`              | Building or running Python (`pystreams/`) Pub/Sub subscribers, NLP/ML use cases |
| `gram-telemetry-query-dimensions` | Adding telemetry query group/filter attributes                                  |
| `feature-flag`                    | Deciding between `productfeatures` vs PostHog flags, or adding either           |
| `glint`                           | Authoring or editing analyzers in the `glint/` go/analysis package              |
| `mise-tasks`                      | Creating or editing mise task scripts in `.mise-tasks/`                         |
| `jaeger`                          | Testing backend endpoints locally and inspecting traces via Jaeger API          |
| `pitchfork`                       | Starting/stopping/restarting local dev services or querying their logs          |
| `datadog`                         | Investigating errors, performance, incidents, or telemetry via Datadog          |
| `datadog-insights`                | Running the full Gram production health digest and posting it to Slack          |
| `spec`                            | Interviewing user in-depth to produce a detailed spec before building           |
| `page-toolbar`                    | Dashboard list page search, filters, sort, or view controls                     |
| `gram-playwright-cli`             | Browser automation, dashboard inspection, screenshots, and page interaction     |
| `pr`                              | Creating a Pull Request for current changes                                     |
| `pr-demo-gif`                     | Recording a demo GIF of a user-visible frontend change for a PR comment         |

# Plan Mode

- Make the plan extremely concise. Sacrifice grammar for the sake of concision.
- Identify any of the skills above that are relevant to the task so you can activate when implementing.
- At the end of each plan, give me a list of unresolved questions to answer, if any.

## Cursor Cloud specific instructions

Full environment setup is handled by `./zero --agent` (idempotent — re-run any time to reconcile): it installs tools/deps, generates keys/TLS + the dev-idp RSA key, starts the Docker infra, and runs the Postgres + ClickHouse migrations and finally starts all local services. Run it per session after starting the Docker daemon. It is deliberately NOT the startup update script — that stays minimal (`mise install` / `mise run install`), because starting infra and running migrations are too heavy and failure-prone for pod boot. Non-obvious caveats:

- **Docker daemon must be running first.** There is no systemd auto-start, so run `sudo service docker start` before `./zero --agent`. Docker is configured with the `fuse-overlayfs` storage driver and `iptables-legacy`.
- **`mise` provides all tooling** (`~/.local/bin/mise`). Resolution is automatic inside `mise run` / `mise exec` and mise tasks (including `.mts` Node scripts) — no PATH hacks needed. For bare tool calls, shims are on `PATH` via `mise activate` in `~/.bashrc` (interactive) and via `~/.bash_env` referenced by `BASH_ENV` (non-interactive _script_ shells). Bash does NOT source `BASH_ENV` for `bash -c`, so in that context prefer `mise exec` / `mise run` (or `export PATH="$HOME/.local/bin:$PATH"`).
- **Login is credential-less** (`GRAM_IDP_MODE=mock-workos`): click "Login", no username/password.
- **Pitchfork manages services**: Either use the pitchfork mcp if running or fall back to the `pitchfork` CLI. These both give you access to service health and logs.
