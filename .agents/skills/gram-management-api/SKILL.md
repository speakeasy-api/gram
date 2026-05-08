---
name: gram-management-api
description: Concepts, external interfaces, and conventions for Gram's management API — the Goa-designed HTTP-RPC surface under `/rpc/<service>.<method>` that powers the dashboard, CLI, and public SDK. Activate whenever the task involves designing, implementing, or modifying a management endpoint (new service, new method, payload/result changes, OpenAPI/SDK surface changes, CLI changes, wiring a new service into the server).
metadata:
  relevant_files:
    - "server/design/**/*.go"
    - "server/internal/*/impl.go"
    - "server/internal/*/queries.sql"
    - "server/internal/*/setup_test.go"
    - "server/internal/mv/**/*.go"
    - "server/cmd/gram/start.go"
    - ".changeset/**/*.md"
---

Gram's management API is the internal HTTP-RPC surface that the dashboard, CLI, and SDK use to administer projects, toolsets, deployments, access, and related resources. Every endpoint lives at `/rpc/<service>.<method>`, is authored in Goa DSL under `server/design/`, implemented in a single `Service` struct per package under `server/internal/<service>/`, and exposed through generated server stubs, OpenAPI, CLI bindings, and a TypeScript SDK.

## Concepts and terminology

**Service.** A named collection of related endpoints (e.g. `remoteMcp`, `access`, `auditlogs`). Each service maps one-to-one to a Go package of the same name.

**Method.** A single endpoint on a service. Exposed as `/rpc/<service>.<method>`.

**Payload / Result.** The input and output types for a method. Payloads are composed from shared security payloads plus method-specific form attributes.

**Security scheme.** The authentication mechanism a method accepts. Gram's management endpoints use three schemes: **Session** (browser cookie), **ByKey** (API key header), and **ProjectSlug** (project-selector header). Additional schemes exist for non-management surfaces and are out of scope here.

**Model views (`mv`).** Stateless functions that convert database row types into API response types. Keep database types out of the API boundary — handlers always return a view, never a `repo` struct.

**Handler.** One method implementation on the `Service` struct.

**Management API vs public SDK.** The same Goa design produces two OpenAPI outputs: an internal spec used to generate the TypeScript SDK that powers the dashboard and CLI, and a public spec derived from it via redaction overlays. Only the internal SDK sees every endpoint.

**Changeset.** A short changelog file written alongside a change that identifies which package bumps (server, dashboard, sdk) and by how much (`patch`, `minor`, `major`).

## Server

The design, implementation, tests, and per-service generated code for every management endpoint live under `server/`.

### Conventions

**Naming.** Goa service names (e.g. `remoteMcp`, `auditlogs`) and method names (e.g. `createServer`) are camelCase. DSL types (e.g. `CreateServerForm`, `RemoteMcpServer`) are PascalCase. Go package names under `server/internal/` and `server/design/` are lowercase with no separators (e.g. `remotemcp`).

**Design layout.** Service DSL lives at `server/design/<svc>/design.go`. The service package must be blank-imported in `server/design/gram.go` (alphabetised) for the generator to pick it up.

**HTTP methods.** Read methods use `GET`; mutations use `POST`. Deletes that carry only an id query parameter use `DELETE`.

**Security DSL helpers.** `server/design/security/` exports the `Session`, `ByKey`, and `ProjectSlug` schemes plus paired helpers — `SessionPayload()`/`SessionHeader()`, `ByKeyPayload()`/`ByKeyHeader()`, `ProjectPayload()`/`ProjectHeader()` — that attach the right payload field and HTTP header to a method.

**Security composition.** Most management endpoints advertise `Session` and `ByKey` side-by-side via repeated `Security(...)` calls on the service so the dashboard and API-key clients can both reach them. Project-scoped endpoints additionally layer `ProjectSlug` via `ProjectPayload()` / `ProjectHeader()` so the caller must name a project.

**Shared errors.** Every service calls `shared.DeclareErrorResponses()` (from `server/design/shared/errors.go`) exactly once so the standard Gram error envelope applies to every method.

**Shared types.** When a payload or result type is reused across services, add `Meta("struct:pkg:path", "types")` so the generator emits it under `server/gen/types/` instead of the per-service package.

**OpenAPI meta keys.** Every method has three metadata keys that control the downstream SDK/CLI names:

- `Meta("openapi:operationId", "verbNounSubject")` — OpenAPI operation id.
- `Meta("openapi:extension:x-speakeasy-name-override", "methodName")` — method name on the SDK service class.
- `Meta("openapi:extension:x-speakeasy-react-hook", '{"name": "HookName"}')` — React Query hook name.

**`impl.go` layout.** Lives at `server/internal/<svc>/impl.go`. Contains the `Service` struct (injected dependencies — tracer, logger, database pool, `*auth.Auth`, `*access.Manager`, plus feature-specific fields), compile-time assertions (`var _ gen.Service = (*Service)(nil)`, and `var _ gen.Auther = (*Service)(nil)` when `ByKey` is advertised), a `NewService(...)` constructor, an `Attach(mux, service)` wiring function, `APIKeyAuth` when `ByKey` is advertised, and one method per Goa `Method` declaration.

**Model view files.** `server/internal/mv/<svc>.go` (singular or per-resource — e.g. `remotemcpserver.go`). Exported functions are named `Build<Subject>View` and `Build<Subject>ListView`. The package doc calls these "model views", which is where the package name comes from.

**Wiring.** New services are attached in `server/cmd/gram/start.go` with `<svc>.Attach(mux, <svc>.NewService(...))` near the other service attachments.

**SQLc.** Per-service queries live in `server/internal/<svc>/queries.sql`. Every new service requires a stanza in `server/database/sqlc.yaml` pointing at its queries file and writing to `server/internal/<svc>/repo/`.

**Resource URNs.** Every resource owned by the service gets a URN type under `server/internal/urn/<resource>.go` (template: `server/internal/urn/api_key.go`). URN types wrap `uuid.UUID` and give callers compile-time protection against mixing ids from different subjects. Audit event structs require them for subject identifier fields (see `gram-audit-logging`); other consumers can adopt them opportunistically.

**Test layout.** Tests live in `package <svc>_test` (black-box). A single `setup_test.go` per service defines `TestMain` (calling `testenv.Launch`) and a `newTestService(t)` factory that clones a fresh test database, seeds an auth context, and returns the live `*Service`. Package-local helpers typically include `withExactAccessGrants` for scope setup and `requireOopsCode` for error-shape assertions. One `<method>_test.go` per handler.

### Common code flows

**Handler skeleton.** Inside each method: extract `authCtx` from context → `s.access.Require(...)` → validate inputs → repo work → return `mv.Build<Subject>View(...)`. Handlers never return `repo` types directly. For mutation handlers, wrap the repo work in a transaction — `s.db.Begin(ctx)` → `defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })` → repo writes → `audit.Log*` → `dbtx.Commit(ctx)` — so the audit row and the state it describes commit atomically. Read handlers typically don't need a transaction or audit call.

**Cascading soft-deletes.** When a delete handler tombstones a parent row whose children reference it, the children have to be soft-deleted in the same handler. `ON DELETE CASCADE` foreign keys only fire on hard deletes, so a soft-deleted parent otherwise leaves orphan children that still resolve in the active set and point at a tombstone. Run the child cleanup inside the same `dbtx` as the parent write so the cascade is atomic with it. Scope the cleanup query by both the parent id and `project_id` (per the `postgresql` skill) and filter on `deleted IS FALSE` so re-deletes are no-ops. If the child subject is independently audited, make the cleanup query `:many` with `RETURNING *` and emit one `audit.Log<Verb>` per affected row before the parent's audit event — see "How to audit a cascading delete of child resources" in `gram-audit-logging`. Tests should both assert no active children remain pointing at the deleted parent and assert the expected child audit-event delta.

**`Attach` wiring.** `func Attach(mux goahttp.Muxer, service *Service)` constructs `gen.NewEndpoints(service)`, layers the `middleware.MapErrors()` and `middleware.TraceMethods(tracer)` middleware, and mounts the endpoints via `srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))`.

### Non-generated files

Substitute the service name for `<svc>` and the method name for `<method>`.

| Path                                           | Purpose                                                                                                               |
| ---------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `server/.golangci.yaml`                        | Per-file linter exceptions.                                                                                           |
| `server/cmd/gram/start.go`                     | Wires every service into the HTTP mux.                                                                                |
| `server/design/<svc>/design.go`                | The service's Goa design. Regenerates `server/gen/<svc>/` and `server/gen/http/<svc>/` via `mise run gen:goa-server`. |
| `server/design/gram.go`                        | Root Goa import graph. Regenerates the aggregate `server/gen/**` tree via `mise run gen:goa-server`.                  |
| `server/design/security/`                      | Shared security schemes and helpers.                                                                                  |
| `server/design/shared/`                        | Design types shared across services.                                                                                  |
| `server/internal/<svc>/<method>_test.go`       | Black-box test file, one per method.                                                                                  |
| `server/internal/<svc>/impl.go`                | The service implementation.                                                                                           |
| `server/internal/<svc>/queries.sql`            | The service's SQLc queries. Regenerates `server/internal/<svc>/repo/` via `mise run gen:sqlc-server`.                 |
| `server/internal/<svc>/setup_test.go`          | Shared test harness for the service.                                                                                  |
| `server/internal/<svc>/shared.go` (or similar) | Non-trivial helpers specific to the service.                                                                          |
| `server/internal/mv/<svc>.go`                  | View builders for the service's response types.                                                                       |

### Generated files

Files under `server/gen/**` and any `repo/` subdirectory carry a `DO NOT EDIT` header.

| Path                                                                                                         | Generator                                                                                                                      |
| ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------ |
| `server/gen/<svc>/{service.go, client.go, endpoints.go}`                                                     | `mise run gen:goa-server` from `server/design/<svc>/design.go`.                                                                |
| `server/gen/http/<svc>/{server,client}/{server.go, client.go, cli.go, encode_decode.go, paths.go, types.go}` | `mise run gen:goa-server` from `server/design/<svc>/design.go`.                                                                |
| `server/gen/types/`                                                                                          | `mise run gen:goa-server` — aggregates types across services that mark `Meta("struct:pkg:path", "types")`.                     |
| `server/internal/<svc>/repo/{db.go, models.go, queries.sql.go}`                                              | `mise run gen:sqlc-server` from `server/internal/<svc>/queries.sql` (via the service's stanza in `server/database/sqlc.yaml`). |

## Server-client contract

The server's design authors the contract; clients receive it through generated OpenAPI, a TypeScript SDK, and CLI bindings. The contract is versioned through changesets.

**HTTP routes.** `/rpc/<service>.<method>`. Method names are derived from the Goa `Service`/`Method` names — renaming them is a breaking change for CLI and any direct HTTP callers.

**OpenAPI.** `server/gen/http/openapi3.yaml` and `openapi3.json` are the raw Goa outputs. `.speakeasy/workflow.yaml` defines two sources (`Gram-Internal`, `Gram-Public`) that apply overlays from `.speakeasy/overlays/` to produce `.speakeasy/out.openapi.yaml` and `.speakeasy/openapi-public.yaml`. The TypeScript SDK is generated from the internal spec.

**TypeScript SDK.** `client/sdk/` — per-operation modules under `src/funcs/`, a per-service class under `src/sdk/`, React Query hooks under `src/react-query/`, and model types under `src/models/{components,operations}/`.

**CLI bindings.** `server/gen/http/cli/gram/cli.go` provides command bindings over the same endpoints.

**Changesets.** `.changeset/<kebab-slug>.md` with YAML frontmatter of the form `"server": minor` (or `"dashboard": patch`, etc.) and a one-paragraph body. A new endpoint is usually `"server": minor`; a bug fix or dashboard-only change is `patch`.

**Regeneration.** After any design change, run `mise run gen:goa-server` (server stubs, OpenAPI, CLI), then `mise run gen:sdk` (Speakeasy overlays, TypeScript SDK, the public OpenAPI output). `gen:sdk` accepts `--check` (fail if outputs drift), `--skip-versioning` (no version bump during iteration), and `--skip-upload-spec` (skip the Speakeasy registry upload).

## Jobs to be done

### How to add a new method to an existing service

1. Edit the service's `server/design/<svc>/design.go` — add a `Method("<method>", ...)` block with `Payload`, `Result`, `HTTP(...)`, and the three OpenAPI meta keys.
2. Run `mise run gen:goa-server` to regenerate the service interface and HTTP server code.
3. Implement the new method on the service in `server/internal/<svc>/impl.go`, following the handler skeleton.
4. Add any required SQLc queries to `server/internal/<svc>/queries.sql` and run `mise run gen:sqlc-server`.
5. Add a view builder in `server/internal/mv/<svc>.go` if the response introduces a new shape.
6. Gate the handler with an existing RBAC scope (see `gram-rbac` skill) and emit audit events for mutations (see `gram-audit-logging` skill).
7. Write a `<method>_test.go` file covering the happy path plus the RBAC-deny and not-found / bad-request paths.
8. Run `mise run lint:server` and `mise run test:server ./internal/<svc>/...`.
9. Run `mise run gen:sdk` to propagate the change into the TypeScript SDK and OpenAPI outputs.
10. Add `.changeset/<kebab-slug>.md` with `"server": minor`.

### How to add a brand-new management API service

1. Add a blank import for the new package in `server/design/gram.go` (alphabetised).
2. Create `server/design/<svc>/design.go` with the service declaration, `Security(...)` calls, `shared.DeclareErrorResponses()`, and the initial set of methods.
3. Add a stanza in `server/database/sqlc.yaml` pointing at `server/internal/<svc>/queries.sql` and writing to `server/internal/<svc>/repo`.
4. Add `server/internal/urn/<resource>.go` (and a test file) for each new resource the service owns, following the `server/internal/urn/api_key.go` template.
5. Author `server/internal/<svc>/queries.sql`.
6. Run `mise run gen:goa-server` and `mise run gen:sqlc-server` (or `mise run gen:server` which runs both).
7. Implement `server/internal/<svc>/impl.go`: `Service` struct, compile-time assertions, `NewService`, `Attach`, `APIKeyAuth` (when `ByKey` is advertised), and each method handler.
8. Add view builders in `server/internal/mv/<svc>.go`.
9. Wire the service into `server/cmd/gram/start.go` with `<svc>.Attach(mux, <svc>.NewService(...))` near the other attachments.
10. Add `server/internal/<svc>/setup_test.go` plus one `<method>_test.go` per handler.
11. Add any new RBAC scopes (`gram-rbac`) and audit subjects (`gram-audit-logging`) the service needs.
12. Run `mise run lint:server`, `mise run test:server`, and `mise run gen:sdk`.
13. Add `.changeset/<kebab-slug>.md` with `"server": minor`.
14. Run `mise run go:tidy` if imports changed.

### How to change a payload or result type

1. Edit the type in `server/design/<svc>/design.go`. Prefer additive changes: new `Attribute` calls on existing types are backwards compatible; `Required("...")` additions and attribute removals are not.
2. Run `mise run gen:goa-server`.
3. Update the handler in `server/internal/<svc>/impl.go` to populate or consume the new fields. `exhaustruct` will flag missed struct fields at lint time.
4. Update the view builder in `server/internal/mv/<svc>.go` if the response type changed.
5. Run `mise run gen:sdk` to propagate to the SDK surface, then update any dashboard consumers (see `frontend` skill).
6. Update tests to cover the new shape.
7. Add `.changeset/<kebab-slug>.md` — `"server": patch` for additive changes, `minor` for anything consumers would notice.

### How to rename an endpoint or override its SDK / hook name

SDK surface names are controlled by OpenAPI metadata, not by renaming Go symbols.

1. Change `Meta("openapi:operationId", ...)`, `x-speakeasy-name-override`, and/or `x-speakeasy-react-hook` on the `Method` in the design file.
2. Run `mise run gen:goa-server` then `mise run gen:sdk` to regenerate the SDK and hook names.
3. Update dashboard consumers to use the new names (see `frontend` skill).
4. Note that the HTTP path is derived from the Goa `Service`/`Method` names — changing those is a breaking change for CLI and any direct HTTP callers. Prefer adding a new method and deprecating the old.

### How to regenerate outputs after a design change

Order matters because each step's output is the next step's input.

1. `mise run gen:goa-server` — after any edit under `server/design/**`.
2. `mise run gen:sqlc-server` — after any edit under `server/internal/*/queries.sql` or `server/database/sqlc.yaml`. Requires the local Postgres container from `mise run infra:start` (sqlc connects to the database to type-check queries). (Or use `mise run gen:server` to run both.)
3. `mise run lint:server` and `mise run test:server` — catch struct-field drift early.
4. `mise run gen:sdk` — regenerate the TypeScript SDK and the public/internal OpenAPI files.
5. `mise run go:tidy` if imports changed.

## Relevant mise tasks

| Task                       | Purpose                                                                                                                                                         |
| -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `mise run build:server`    | Build the server binary.                                                                                                                                        |
| `mise run gen:goa-server`  | Regenerate everything under `server/gen/**` from the Goa design files.                                                                                          |
| `mise run gen:sdk`         | Apply Speakeasy overlays and regenerate the TypeScript SDK and both public/internal OpenAPI files. Flags: `--check`, `--skip-versioning`, `--skip-upload-spec`. |
| `mise run gen:server`      | Convenience: runs `gen:sqlc-server` then `gen:goa-server`.                                                                                                      |
| `mise run gen:sqlc-server` | Regenerate every `repo/` package from every `queries.sql`. Requires `mise run infra:start` (sqlc connects to the local Postgres to type-check queries).         |
| `mise run go:tidy`         | `go mod tidy` across the workspace.                                                                                                                             |
| `mise run lint:server`     | `golangci-lint` over the server tree including `exhaustruct`.                                                                                                   |
| `mise run test:server`     | Runs `go test` across the server tree; accepts `go test` arguments.                                                                                             |

## Maintaining this skill

This file documents conventions that evolve over time. Adding a new endpoint or service using the patterns above is already covered by "Jobs to be done" — those don't require skill edits. Structural changes do. Update this skill in the same commit when you make any of the following kinds of changes:

- Changing the `/rpc/<service>.<method>` HTTP route convention, or introducing a second route convention alongside it.
- Adding, removing, or renaming a security scheme (`Session`, `ByKey`, `ProjectSlug`).
- Changing the required OpenAPI meta keys or their semantics.
- Replacing `mv/` with a different view pattern, or changing the `Build<Subject>View` naming convention.
- Changing the `impl.go` anatomy — new required struct fields, new middleware layer in `Attach`, a different `NewService` contract.
- Changing the black-box test convention (`package <svc>_test`, `testenv.Launch`, `newTestService`) or moving shared helpers out of per-service `setup_test.go`.
- Changing the Speakeasy workflow — new overlay stages, different source definitions in `.speakeasy/workflow.yaml`, a third OpenAPI output alongside internal/public.
- Changing the changeset format or adding a new package target.
- Adding a new API-relevant mise task that belongs on the cheat sheet.

## Cross-references

- `golang` — Go code style, error handling (`oops`), logging, testing, dependency injection, and the `conv` / `o11y` helpers referenced in the handler skeleton.
- `postgresql` — schema design, migrations, SQLc query rules, and the `project_id` scoping requirement.
- `gram-rbac` — scope declaration and `access.Require(ctx, access.Check{...})` enforcement inside handlers.
- `gram-audit-logging` — emitting `audit.Log*` calls inside handler transactions.
- `frontend` — consuming endpoints from the dashboard via the generated SDK and React Query hooks.
- `mise-tasks` — modifying any of the `.mise-tasks/gen/*.sh` scripts referenced above.
