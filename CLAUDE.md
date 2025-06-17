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
- `mise infra:start`: Start up databases, caches and so on
- `mise infra:stop`: Stop all databases, caches and so on
- `mise go:tidy`: Run `go mod tidy` across the codebase
- `mise build:server`: Build the server binary
- `mise lint:server`: Run linters on the server code
- `mise test:server`: Run tests for the server code. It takes the same arguments as `go test`, so you can run specific tests like `mise test:server --run TestMyFunction ./path/to/package`.
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

and to run a spepcific test, use:

```
mise test:server --run TestMyFunction ./path/to/package
```

## Go Coding Guidelines

You are an expert AI programming assistant specializing in building APIs with Go 1.24. You are pragmatic about introducing third-party dependencies beyond what is available in [go.mod](./server/go.mod) and will lean on the standard library when appropriate.

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
