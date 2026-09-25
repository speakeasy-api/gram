---
name: golang
description: Use when writing, editing, or reviewing Go (Golang) code in this repo, including tests, Goa services, sqlc callers, Temporal activities, and CLI code under server/, cli/, or functions/.
metadata:
  relevant_files:
    - "server/**/*.go"
    - "functions/**/*.go"
    - "cli/**/*.go"
---

These are the conventions for Go code in this repo, which targets the Go version in the root [go.mod](../../../go.mod). Some existing code breaks these conventions; do not copy it.

**Scope.** The language conventions (comments, constants and types, functions, errors, logging style, testing style) apply to all Go in the repo. The package map, service wiring, and `testenv` guidance describe `server/`; its `internal` packages cannot be imported from `cli/` or `functions/`. In those trees, use the accessible local equivalent (Functions logging attributes live in `functions/internal/attr`) and search the target tree before introducing a helper or dependency. Do not export server internals or add a shared package just to follow this skill.

- Prefer the standard library and the dependencies already in `go.mod` over adding a new one.
- Do not edit files that start with a "DO NOT EDIT" comment; change their source and regenerate.
- If you are unsure about a convention or implementation detail, say so instead of guessing.

## Tooling

Never invoke `go` bare. Use a `mise` task when one exists: for server tests, `mise run test:server` runs from `server/` and accepts the same arguments as `go test` (`mise run test:server ./internal/oops/`). Otherwise prefix the command with `mise exec --` (`mise exec -- go test ./server/internal/oops/`). A bare `go` may resolve to a system toolchain that differs from the mise pin, which fails every stdlib compile with a `version … does not match go tool version` error. The same applies to the other pinned tools (`golangci-lint`, `gotestsum`, `sqlc`, `gomigrate`) and to `goa`, which is a `go.mod` tool directive and inherits whichever toolchain invoked it.

Run `mise lint:server` before finishing server changes and fix what it reports. `cli/` and `functions/` do not run the `glint` analyzers, so follow this skill there by hand. Suppress a finding only when the violation is intentional, with a specific reason. Put the directive as a trailing comment on the reported line (for a whole test, the `func` line), or, for a multi-line statement inside a function, either on its own line directly above it or trailing its first line; `glint` directives may not sit above the `package` clause or a top-level declaration. Ordinary linters are named directly; `glint` findings also name the analyzer at the start of the reason:

```go
func TestConfigFromEnv(t *testing.T) { //nolint:paralleltest // mutates process environment with t.Setenv
opts := LaunchOptions{Postgres: true} //nolint:exhaustruct // negative fixture omits the other services on purpose
//nolint:glint // notestingrawsql: pg_blocking_pids is a PostgreSQL synchronization primitive SQLc cannot generate
err := conn.QueryRow(ctx, blockingPIDsSQL, pid).Scan(&blockers)
```

## Which skill

When a change touches one of these areas, activate the matching skill as well:

| Task                                                                                                                      | Skill                             |
| ------------------------------------------------------------------------------------------------------------------------- | --------------------------------- |
| ClickHouse schemas, migrations, queries, or inserts                                                                       | `clickhouse`                      |
| Gating a feature behind a product feature or PostHog flag                                                                 | `feature-flag`                    |
| New or changed lint rules (`glint` analyzers) or their fixtures                                                           | `glint`                           |
| Recording or exposing audit events                                                                                        | `gram-audit-logging`              |
| Features that surface data in the dashboard (demo and local seed data)                                                    | `gram-demo-seed`                  |
| Gram Functions runner (`functions/`) or `server/internal/functions`                                                       | `gram-functions`                  |
| Goa management endpoints under `/rpc/<service>.<method>`                                                                  | `gram-management-api`             |
| Pub/Sub topics, publishers, or stream handlers                                                                            | `gram-pubsub`                     |
| Scopes, grants, roles, or `authz.Engine.Require` checks                                                                   | `gram-rbac`                       |
| New group-by or filter dimensions in `telemetry.query`                                                                    | `gram-telemetry-query-dimensions` |
| Temporal workflows, activities, schedules, signals, or per-row, timer, and fan-out background work                        | `gram-temporal`                   |
| Admin dashboard workflows, backend admin APIs, or staff permissions (`server/internal/admin`, `server/internal/adminmcp`) | `maintaining-admin-mcp`           |
| Platform MCP tools, or backend changes that may need one                                                                  | `maintaining-platform-mcp`        |
| Postgres schema, migrations, `queries.sql`, or sqlc                                                                       | `postgresql`                      |
| Transactional email templates, sending, layout, or copy                                                                   | `transactional-email`             |

## Packages

### Reuse existing packages

Before writing a helper, fake, or client, search the current package (including its `setup_test.go`) and the target tree for one that already does the job; grep for the concept (an RFC number, a vendor name). Extend what you find rather than adding a near-duplicate beside it, and when the owning package lacks a variant you need, add it there.

Reusable packages in `server/internal`:

| Need                                                            | Use                                                                                                                                                                                                 |
| --------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Auth and request context                                        | `contextvalues.GetAuthContext`, `contextvalues.GetRequestContext`                                                                                                                                   |
| Caching                                                         | `cache.NewTypedObjectCache`, with `cache.NoopCache` where caching is off                                                                                                                            |
| Conversions: pointers, defaults, pgtype, UUIDs, slugs, integers | `conv` (`ToPGText`, `PtrToPGText`, `FromPGText`, `PtrValOr`, `Default`, `SafeInt32`, ...)                                                                                                           |
| Encrypting stored secrets                                       | `encryption`                                                                                                                                                                                        |
| Invariant checks                                                | `inv.Check`, `inv.Require`                                                                                                                                                                          |
| OAuth wire parameters and error codes                           | `oauthwire`, `oautherr` (`oautherr.ParseTokenError` for error bodies)                                                                                                                               |
| Outbound HTTP and retries                                       | `guardian.Policy.Client(...)` or `PooledClient(...)`, with `guardian.WithRetryConfig` instead of hand-written retry loops. Inside a Temporal activity, add no HTTP retries; Temporal retries it.    |
| Rate limits and bounded concurrency                             | `ratelimit.New` for budgets; `errgroup.SetLimit` to bound concurrent work instead of a channel semaphore                                                                                            |
| Resource identifiers                                            | `urn` types, never hand-built URN strings                                                                                                                                                           |
| Test doubles                                                    | See [Testing](#testing)                                                                                                                                                                             |
| Test infrastructure and cross-package fixtures                  | `testenv`                                                                                                                                                                                           |
| Transactional events and customer webhooks                      | `outbox.Publish`, `outbox.PublishWebhookEvent`, published in the transaction that records the state change behind the event; define new webhook events with `outbox.NewEventDef` in `outbox/events` |
| User-facing errors in handlers                                  | `oops.E` with the `oops.Code*` constants                                                                                                                                                            |

### Package boundaries

Give a cohesive domain its own subpackage (e.g. `dpop`, `remotesessions/delegation`) instead of growing a large parent package. Domain types live in the domain package, not in the metrics, view, or handler package that happened to need them first.

## Comments

Comments describe current intent and behavior.

- Do not narrate the edit you just made ("previously this returned X, now it returns Y", "renamed from Foo"); that belongs in the commit message or PR body. Build tags, `//go:` directives, and `TODO(TICKET-123)` notes are not narration.
- Mark a deprecated symbol with a trailing `Deprecated: use X instead` paragraph (the form `gopls` and pkg.go.dev surface), and keep the rest of its doc in the present tense.
- Do not write "see X" comments that send the reader elsewhere ("see `Registry` for locking rules"); the target moves and the pointer goes stale. State the fact where the reader needs it ("callers must hold `Registry.mu`"). Referencing a symbol, ticket, or spec that the comment is about is fine. When the full explanation is too long to restate, document it on the type or package that owns the invariant and give each use site the one-line consequence.
- Document each struct field with its own comment, at least on exported types. `gofmt` does not catch the mistakes here: a comment above only the first field of a group documents just that field, so `go doc` and editor hovers show nothing for the rest, and a blank line between a comment and its field silently detaches it. Put a blank line before each field's comment after the first. Grouped `const`/`var` blocks and interface methods follow the same rule, and a comment above `const (` documents the block, not its specs.

```go
type Config struct {
    // ReadTimeout bounds how long the client waits for response headers.
    // Retries each get a fresh budget, so a request can exceed this in total.
    ReadTimeout time.Duration

    // WriteTimeout bounds how long the client spends sending the request body.
    WriteTimeout time.Duration
}
```

## Constants and types

- **Name and explain numeric values.** Limits, sizes, budgets, timeouts, retry counts, and thresholds are named constants, each with a comment saying what the value represents and why it was chosen. Write bitwise and multiplied sizes with the human-readable value alongside. Obvious literals (`0`, `1`, `2` for halving or pairs, base `10` in `strconv`) need no name.

  ```go
  const (
      // maxManifestBytes bounds a plugin manifest upload. Real manifests are
      // under 64 KiB; 1 MiB leaves headroom without letting one request
      // hold a large buffer.
      maxManifestBytes = 1 << 20 // 1 MiB

      // syncRetryTTL is how long a failed sync waits before retrying. The
      // upstream API rate limits per hour, so retrying sooner cannot succeed.
      syncRetryTTL = time.Hour
  )
  ```

- **Fully initialize struct literals that `exhaustruct` covers.** Its excluded types and paths (test files among them) are listed in `server/.golangci.yaml`. When adding a field, update every covered literal, including zero values (`MyStruct{}` becomes `MyStruct{NewField: nil}`).
- **Use `new(expr)` for a pointer to a value** (Go 1.26+, e.g. `new("secret")`) instead of a local `ptr` helper.
- **Use string enums.** Declare `type fooOutcome string` with string constants so values go straight into logs and metric dimensions. If the zero value means "unknown", declare that constant explicitly rather than relying on `""`.
- **Use struct types for context keys.** Declare a distinct unexported type per key (`type domainKey struct{}`) so keys from different packages cannot collide.
- **Do not alias constants or variables.** If another package or an external test needs one, export it where it is declared (including instead of an `export_test.go` alias). Keep a separate name only when it deliberately decouples from its source: it selects a policy that must stay a one-line change (`const DefaultInEffect = Version20250326`), or it names a separate concept that shares the value today but may diverge. Say in its doc comment why the name must stay separate.
- **Use protocol constants, not string literals.** If a string's meaning comes from an RFC or spec (OAuth parameters and error codes, HTTP statuses and methods), use the constant from its owning package (see [Reuse existing packages](#reuse-existing-packages), and `net/http` for HTTP), and add one there if it is missing.
- **Type contractual JSON.** A JSON object whose shape is defined by a standard or an external contract gets a named struct type with a comment on each field, not a `map[string]any`.

## Functions

- Take `context.Context` as the first argument of any function that does I/O, runs queries, or waits. To wait, use a `time.Timer` or `time.After` in a `select` on `ctx.Done()`, a Temporal timer, or `ratelimit`. In Temporal workflow code, use `workflow.Now(ctx)` for the current time.
- Pass time-dependent logic a `now time.Time` argument, or give its struct a `now func() time.Time` field, instead of calling `time.Now()` inside, so tests control the clock.
- Extract a function only when it adds reuse, names non-trivial logic, adapts a type to a required interface, or provides a seam that tests need. A function that only forwards to another call does none of these, in production code or tests (`func newFoo(t) *Foo { return NewFoo(deps) }`, `func seedX(...) { repo.New(db).InsertX(...) }`). When a signature changes, update the callers instead of adding a wrapper that keeps the old signature.

```go
// Bad: the wrapper adds no abstraction and has one caller.
func (s *Service) listWidgets(ctx context.Context) error {
    return s.repo.ListWidgets(ctx)
}

func (s *Service) List(ctx context.Context) error {
    return s.listWidgets(ctx)
}
```

Call `s.repo.ListWidgets(ctx)` directly from `List`.

## Errors

- In low-level functions, wrap with `fmt.Errorf` and a short context unique to the call site: `fmt.Errorf("save user: %w", err)`. Skip "failed to" and generic context such as "run database query".
- In `server/` handlers and other high-level functions, use `oops` to pair the internal error with a user-facing message. Log client faults (4xx codes) with `LogWarn` or `LogInfo` and server faults (e.g. `oops.CodeUnexpected`) with `LogError`, so client mistakes do not mark the trace span as errored:

  ```go
  return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogWarn(ctx, s.logger)
  ```

- When callers need to branch on an error, export a sentinel (`var ErrNotFound = errors.New("...")`) or an error type, and have callers match it with `errors.Is` or `errors.As`.

## Logging and observability

- Build attributes with the tree's attribute helpers, not raw keys: `server/internal/attr/conventions.go` in `server/`, `functions/internal/attr` in `functions/`. Add a helper there when none fits. Always include the error: `logger.ErrorContext(ctx, "write to database", attr.SlogError(err))`.
- Create child loggers with `logger.With(...)` to carry context into later calls (`logger.With(attr.SlogProjectID(projectID))`).
- Log errors where they are handled and keep info-level logs rare.
- In `server/`, run deferred cleanup through `o11y` rather than a bare `defer x.Close()` or `defer func() { _ = x.Close() }()`; the linter does not catch this:
  - `defer o11y.LogDefer(ctx, logger, "close export file", func() error { return file.Close() })` when a failure matters (file I/O, critical resources). The message names the operation and resource.
  - `defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })` when a failure is expected or harmless (rollback after commit, closing a response body).

## Services

### Goa services

Goa designs live in `server/design` (DSL reference: `https://pkg.go.dev/goa.design/goa/v3/dsl`). After editing a design, run `mise run gen:goa-server` and use `git` to see what changed under `server/gen`.

Each service lives in its own package with an `impl.go` (`server/internal/<service>/impl.go`). Copy an existing service's `impl.go` as the template; it contains:

- the `Service` struct and a `NewService` constructor
- a `var _ gen.Service = (*Service)(nil)` assertion, plus `var _ gen.Auther = (*Service)(nil)` when the service uses API key security
- an `Attach(mux, service)` function that mounts the endpoints with `middleware.MapErrors()` and `middleware.TraceMethods(service.tracer)`
- for API key security, an `APIKeyAuth` method delegating to `s.auth.Authorize`

Attach new services in `server/cmd/gram/start.go`.

### Dependency injection

Inject dependencies through the service constructor and store them on the struct, not on the sessions manager or other shared state. For database access, inject the pool and build query helpers where they are used; do not add `repo.Queries` fields to a service struct (not lint-enforced).

```go
type Service struct {
    db *pgxpool.Pool
}

func (s *Service) Handler(ctx context.Context) error {
    if err := repo.New(s.db).DoThing(ctx); err != nil {
        return fmt.Errorf("do thing: %w", err)
    }
    return nil
}
```

### Auth context

In organization-scoped handlers, assume `ActiveOrganizationID` is present. The auth middleware handles the brief org-less login boundary by installing zero RBAC grants, so handlers do not need defensive empty-org checks unless a concrete code path proves otherwise.

### Third-party clients

- Make constructors return a usable client (the stub when unconfigured), so callers never check for `nil`. Missing configuration for an optional integration selects the stub; it is not a constructor error.
- When adding a vendor wrapper, add a stub alongside it for local development and tests, and choose real or stub in `server/cmd/gram/deps.go` based on `c.String("environment")`.
- Keep vendor request and response types inside the wrapper. Define our own types at the boundary (`type Client interface { Send(ctx context.Context, message Message) error }`).

## Testing

The [Functions](#functions) and [Reuse existing packages](#reuse-existing-packages) rules apply to test code too. The `setup_test.go`, SQLc fixture, and `testenv` bullets describe `server/`.

- Put setup that starts databases, caches, or workers in the package's `setup_test.go`: a `TestMain` calls `testenv.Launch(ctx, testenv.LaunchOptions{Postgres: true, ...})` into a package-level `infra`, and each test gets an isolated database from `infra.CloneTestDatabase(t, "testdb")`. Copy an existing `setup_test.go` (e.g. `server/internal/access`) and reuse its helpers.
- Seed and inspect Postgres through SQLc, never raw SQL. Add a fixture query to the domain package's own `queries.sql` (a `toolsets` fixture goes in `server/internal/toolsets/queries.sql`). To seed rows owned by another domain, call that domain's generated repo (e.g. `orgrepo.CreateOrganizationMetadata`). Use `server/internal/testenv/queries.sql` and `testenv/testrepo` only for fixtures reused across packages. Genuine exceptions (PostgreSQL synchronization primitives such as `pg_blocking_pids`, or constraint tests that need writes SQLc cannot express) get a line-level `notestingrawsql` suppression.
- Test doubles, in order of preference:
  1. The vendor package's stub or fake when one exists; names vary (`workos.NewStubClient`, `okta.NewFake`, `openrouter.NewDevelopment`).
  2. A suitable fake or capture already in the current package.
  3. A small local fake or capture of an interface we own (for example a recording `loops.Client`). Consolidate one next to its interface only when another package actually needs it; do not relocate unrelated fixtures as part of a narrow change.
  4. `github.com/stretchr/testify/mock` for vendor SDK clients without a stub, against a narrow interface declared where it is consumed when the vendor interface is large.
- `httptest` servers are fine for protocol-level HTTP tests. `guardian.NewDefaultPolicy` blocks loopback, so build the client from `guardian.NewUnsafePolicy(tp, nil)` in those tests.
- Use `testenv.NewLogger(t)`, `testenv.NewTracerProvider(t)`, and `testenv.NewMeterProvider(t)` instead of inline loggers or noop providers. `testenv.NewLogger(t)` discards in normal runs and pretty-prints under `go test -v`. Tests that assert on log output use a capturing handler over a `bytes.Buffer` instead.
- Use `t.Context()` instead of `context.Background()`, except inside `t.Cleanup` callbacks.
- Assert with `github.com/stretchr/testify/require`. The one exception is `assert.*` on the `*assert.CollectT` inside `require.EventuallyWithT`.
- Never `time.Sleep` to wait for async state. When the test owns the goroutine, wait on a channel or `sync.WaitGroup` it signals. Otherwise poll with `require.EventuallyWithT`, or assert absence with `require.Never`. Use `testing/synctest` (`synctest.Test` and `synctest.Wait`) for in-process timer and debounce logic; `time.Sleep` inside the synctest bubble is allowed because it advances the fake clock.
- Write a separate test function for each scenario that needs its own setup or assertions, so it can be read, run with `-run`, and fail on its own. Use a table-driven test only when cases differ just in inputs and expected outputs (a value or an error), running each case with `t.Run(tc.name, ...)` so a failure is named and can be rerun with `-run`.
- Parallelize isolated tests and independent table cases: call `t.Parallel()` first in the test and in each `t.Run` closure (`paralleltest` and `tparallel` report missing calls). Keep a test and its ancestors sequential when it mutates process-global state (`t.Setenv` and `t.Chdir` panic under `t.Parallel()`) or intentionally shares a mutable fixture, and say why with `//nolint:paralleltest // <reason>`.
