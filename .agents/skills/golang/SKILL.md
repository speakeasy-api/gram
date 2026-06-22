---
name: golang
description: Rules and best practices when writing and editing Go (Golang) code
metadata:
  relevant_files:
    - "server/**/*.go"
    - "functions/**/*.go"
    - "cli/**/*.go"
---

This codebases uses features from Go 1.25 and above.

- Be pragmatic about introducing third-party dependencies beyond what is available in [go.mod](./server/go.mod) and lean on the standard library when appropriate.
- Use the Go standard library before attempting to suggest third party dependencies.
- Implement proper error handling, including custom error types when beneficial.
- Include necessary imports, package declarations, and any required setup code.
- Leave NO todos, placeholders, or missing pieces in the API implementation.
- Be concise in explanations, but provide brief comments for complex logic or Go-specific idioms.
- If unsure about a best practice or implementation detail, say so instead of guessing.
- Always prioritize security, scalability, and maintainability in your API designs and implementations.
- Avoid editing any source files that have a "DO NOT EDIT" comment at start of them.
- Store dependencies on service structs via constructor-based dependency injection. Do NOT hide dependencies in session manager state.
- Avoid shallow helpers that are just a one-line wrapper around another method, especially when they are only used once.
- When using a slog logger, always use the context-aware methods: `DebugContext`, `InfoContext`, `WarnContext`, `ErrorContext`.
- When logging errors make sure to always include them in the log payload using `attr.SlogError(err)`. Example: `logger.ErrorContext(ctx, "failed to write to database", attr.SlogError(err))`.
- Any functions or methods that relate to making API calls or database queries or working with timers should take a `context.Context` value as their first argument.
- Always run linters as part of finalizing your code changes. Use `mise lint:server` to run the linters on the server codebase.
- The `exhaustruct` linter requires all struct fields to be explicitly set in struct literals. When adding new fields to a type, update ALL call sites — including places that construct the struct with zero values (e.g., `MyStruct{}` → `MyStruct{NewField: nil}`).

## Updating the API

We use Goa to design our API and generate server code. All Goa code lives in `server/design`. The Goa DSL is documented in `https://pkg.go.dev/goa.design/goa/v3/dsl`.

To make an API change such as creating a new service or update an existing one:

- Update the Goa design files in `server/design` to reflect the API change.
- Run `mise run gen:goa-server`
- This will regenerate the server code in `server/gen` with the new API changes. It's best to use `git` to discover the added/changed files.

When implementing Goa services:

- Ensure the service lives in a separate go package with an impl.go file such as `server/internal/<service>/impl.go`.
- The general layout of the impl.go file should be as follows:

```go
package assets

import (
	"context"

	"log/slog"

	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/assets"
	srv "github.com/speakeasy-api/gram/server/gen/http/assets/server"
	"github.com/speakeasy-api/gram/server/internal/auth"
)

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	auth      *auth.Auth
  // dependencies
}

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
  auth *auth.Auth,
  // dependencies
) *Service {
  return &Service{
    // initialize dependencies
  }
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) ListAssets(ctx context.Context, payload *gen.ListAssetsPayload) (*gen.ListAssetsResult, error) {
  // implementation
}
```

If you are creating a new Goa service, then make sure to attach it to the http server in `server/cmd/gram/start.go`.

## Dependency injection

- Always inject dependencies directly into service structs via the constructor.
- Do NOT use a session manager to stash dependencies that the service needs later.
- When a service needs database access, inject the DB connection and initialize query helpers (`repo.New`) when needed in functions.
- Do NOT store `repo.Queries` directly on a service struct for a new service.

<bad-example>

```go
type Service struct {
    queries *repo.Queries
}

func NewService(db *pgxpool.Pool) *Service {
    return &Service{
        queries: repo.New(db),
    }
}
```

This makes the service depend on a concrete query helper instance up front, which is not the pattern we want for new services.

</bad-example>

<good-example>

```go
type Service struct {
    db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
    return &Service{db: db}
}

func (s *Service) Handler(ctx context.Context) error {
    queries := repo.New(s.db)

    if err := queries.DoThing(ctx); err != nil {
        return fmt.Errorf("do thing: %w", err)
    }

    return nil
}
```

This keeps the service dependency simple and avoids baking `repo.Queries` into the service shape.

</good-example>

## Auth context assumptions

- When reading `authctx`, assume `ActiveOrganisationID` is present.
- Do NOT add defensive empty checks for `ActiveOrganisationID` unless there is a concrete code path proving otherwise.

Avoid patterns that treat `ActiveOrganisationID` as optional when reading `authctx`. That adds defensive code around an invariant that should already hold.

## Third-party clients

- Constructors for third-party clients should always return a usable client implementation.
- Avoid designs where internal code has to repeatedly check whether a client is `nil` before calling it.
- Provide a stub implementation for local development and tests, but choose between the real and stub implementation in `deps.go` based on `c.String("environment")`.
- Do NOT expose third-party request/response types from your wrapper to the rest of our codebase. Define our own types at the boundary.

<bad-example>

```go
type Service struct {
    client *vendor.Client
}

func NewService(cfg Config) *Service {
    if cfg.APIKey == "" {
        return nil
    }

    return &Service{client: vendor.New(cfg.APIKey)}
}

func (s *Service) Send(ctx context.Context, req *vendor.Request) error {
    if s.client == nil {
        return nil
    }

    return s.client.Send(ctx, req)
}
```

This leaks vendor types into internal code and spreads `nil` handling into runtime call paths.

</bad-example>

<good-example>

```go
type Client interface {
    Send(ctx context.Context, message Message) error
}

type Message struct {
    To      string
    Subject string
    Body    string
}

type Service struct {
    client Client
}

func NewService(client Client) *Service {
    return &Service{client: client}
}
```

Wire the real or stub implementation in `deps.go` so the service always receives a valid `Client`, and keep vendor-specific types inside the wrapper implementation.

</good-example>

## Transactional email (Loops)

Sending transactional email goes through `server/internal/email`. The package wraps Loops and enforces a strongly typed `Template` interface.

### Adding a new template

Follow these four steps:

1. Add a `TransactionalID` constant to `server/internal/email/templates.go` — single registry, grep-friendly.
2. Create `server/internal/email/template_<name>.go` with a struct implementing the `Template` interface (`TransactionalID()`, `Variables()`, `AddToAudience()`).
3. Append a zero value of the struct to `RegisteredTemplates` in `templates.go` so tests catch duplicate IDs (e.g. `AccessRequestCreated{}`).
4. Write `server/internal/email/template_<name>_test.go` covering: `TransactionalID` returns the expected constant, `Variables` returns the correct snake_case keys with all keys present, `AddToAudience` returns the expected bool.

To send: call `s.emailSvc.Send(ctx, recipientEmail, tmpl)` where `tmpl` is your populated template struct.

### Variable key naming

`Variables()` must return **snake_case** keys. Loops substitutes these keys directly into template variables — camelCase keys silently render as blank fields in the delivered email.

Every declared key must be present in the returned map even when the value is empty. A missing key causes partial template rendering.

<bad-example>

```go
func (t MyTemplate) Variables() map[string]string {
    return map[string]string{
        "approvalUrl":    t.ApprovalURL,
        "requesterEmail": t.RequesterEmail,
    }
}
```

camelCase keys silently render as blank fields in Loops — no error, no warning.

</bad-example>

<good-example>

```go
func (t MyTemplate) Variables() map[string]string {
    return map[string]string{
        "approval_url":    t.ApprovalURL,
        "requester_email": t.RequesterEmail,
    }
}
```

</good-example>

### `AddToAudience` semantics

Controls whether Loops upserts the recipient as a contact in the audience when the email is sent.

- Return `true` for user-facing emails that are part of the recipient's product journey (team invites, onboarding).
- Return `false` for operational/admin emails where the recipient is incidental (admin alerts, system notifications).

### Testing patterns

**Base test setup — never pass `nil` for `*email.Service`:**

```go
loopsClient := loops.New(ctx, logger, nil, "") // nil guardian policy is safe when key is empty; returns noop client
noopEmailSvc := email.NewService(logger, loopsClient)
```

**Asserting on sent emails — use a capture client:**

`loops.Client` is our own interface (not a vendor type), so a hand-rolled capture client is appropriate here. The capture pattern lets tests assert on the exact payload sent — use it instead of `testify/mock` for Loops email assertions.

```go
type captureLoopsClient struct {
    mu   sync.Mutex
    sent []loops.SendTransactionalInput
}

func (c *captureLoopsClient) SendTransactional(_ context.Context, input loops.SendTransactionalInput) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.sent = append(c.sent, input)
    return nil
}

func (c *captureLoopsClient) Sent() []loops.SendTransactionalInput {
    c.mu.Lock()
    defer c.mu.Unlock()
    out := make([]loops.SendTransactionalInput, len(c.sent))
    copy(out, c.sent)
    return out
}
```

To use it in a test, declare an instance and swap it into the service:

```go
captured := &captureLoopsClient{}
svc.emailSvc = email.NewService(testenv.NewLogger(t), captured)
```

(This assigns an unexported field — works from within the same package, which is the convention for `access` package tests.)

**Optional display fields — use `conv.Default`:**

```go
DisplayName: conv.Default(request.DisplayName, "(unknown resource)"),
```

Never send a template with a blank field that produces broken email copy. Apply a meaningful fallback at the Go layer, not in the Loops template.

## Function shape

- Avoid helper functions and methods that only forward to another method with no meaningful logic.
- Avoid extracting single-use one-liners into separate methods just for indirection.
- Prefer inlining trivial behavior at the call site unless the extracted function adds reuse, naming value, or non-trivial logic.

<bad-example>

```go
func (s *Service) listWidgets(ctx context.Context) error {
    return s.repo.ListWidgets(ctx)
}

func (s *Service) List(ctx context.Context) error {
    return s.listWidgets(ctx)
}
```

The wrapper adds no abstraction and is only used once.

</bad-example>

<good-example>

```go
func (s *Service) List(ctx context.Context) error {
    return s.repo.ListWidgets(ctx)
}
```

</good-example>

## Error handling

In low-level functions, use `fmt.Errorf` to wrap errors with distinct and useful context:

<bad-example>

```go
func SaveUser(repo Repository, u User) error {
  err := repo.Save(u)
  if err != nil {
    return fmt.Errorf("failed to save user: %w", err)
  }
  return nil
}
```

Do not need to use "failed to" language.

</bad-example>

<bad-example>

```go
func SaveUser(repo Repository, u User) error {
  err := repo.Save(u)
  if err != nil {
    return fmt.Errorf("run database query: %w", err)
  }
  return nil
}
```

Do not use generic language that doesn't add any context and doesn't improving searching for errors in the codebase.

</bad-example>

<good-example>

```go
func SaveUser(repo Repository, u User) error {
  err := repo.Save(u)
  if err != nil {
    return fmt.Errorf("save user: %w", err)
  }
  return nil
}
```

This is much better. The error message is concise and to the point and unique to the call site.

</good-example>

In higher-level functions of the `server/` codebase, which include HTTP service handlers, use the `server/internal/oops` package which allows us to wrap internal errors with user-facing error messages.

<good-example>

```go
func (s *Service) ListDeployments(ctx context.Context, form *gen.ListDeploymentsPayload) (res *gen.ListDeploymentResult, err error) {
  var cursor uuid.NullUUID
	if form.Cursor != nil {
		c, err := uuid.Parse(*form.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, s.logger)
		}

		cursor = uuid.NullUUID{UUID: c, Valid: true}
	}
}
```

</good-example>

## Logging

- Use log/slog for logging.
- ALWAYS use logging attributes defined in `server/internal/attr/conventions.go` when logging in the server codebase.
- Where appropriate, create child loggers using `logger.With(attr.SlogXXX(...))` to capture contextual attributes for logging in later parts of code.
- DO NOT spam the codebase with log statements. Focus on logging errors where appropriate and reduce the noise from excessive info-level logs.

<bad-example>

```go
logger.InfoContext(ctx, "user created", "user_id", userID)
```

This is bad because it doesn't use the attributes from the convention package.

</bad-example>

<bad-example>

```go
import "github.com/speakeasy-api/gram/functions/internal/attr"

func Example() {
  logger.Error("failed to create user", attr.SlogError(err))
}
```

This is bad because it uses `logger.Error` instead of `logger.ErrorContext`.

</bad-example>

<good-example>

```go
import "github.com/speakeasy-api/gram/functions/internal/attr"

func Example(ctx context.Context) {
  logger.ErrorContext(ctx, "failed to create user", attr.SlogError(err))
}
```

This is great because:

- It uses `logger.ErrorContext` which is the convention for logging in the server codebase.
- It uses the `attr.SlogError` attribute from the attr package.

</good-example>

## Conversion utilities (`server/internal/conv`)

Use the `conv` package for common type conversions instead of writing inline helpers. Key functions:

- `conv.PtrEmpty(v)` — If v is not the zero value, return a pointer to v; otherwise, return nil.
- `conv.PtrValOr(ptr, default)` — dereference a pointer with a fallback default.
- `conv.Default(val, default)` — return `val` unless it is the zero value, then return `default`.
- `conv.ToPGText`, `conv.ToPGTextEmpty`, `conv.PtrToPGText`, `conv.PtrToPGTextEmpty` — convert strings to `pgtype.Text`.
- `conv.FromPGText`, `conv.FromPGBool` — convert `pgtype` values to Go pointer types.
- `conv.PtrToPGBool` — convert a `*bool` to `pgtype.Bool`.
- `conv.Ternary(cond, trueVal, falseVal)` — inline conditional expression.

<important>

Do NOT reimplement pointer helpers, ternary expressions, or pgtype conversions inline. Always reach for `conv` first.

</important>

## Observability (`server/internal/o11y`)

Use the `o11y` package for deferred cleanup and error logging. Two key functions:

### `o11y.LogDefer`

```go
func LogDefer(ctx context.Context, logger *slog.Logger, cb func() error) error
```

Use `LogDefer` when a cleanup operation's error should be **logged**. Wrap cleanup calls with `defer o11y.LogDefer(...)` so failures are always visible in logs.

<good-example>

```go
defer o11y.LogDefer(ctx, logger, func() error { return file.Close() })
```

</good-example>

### `o11y.NoLogDefer`

```go
func NoLogDefer(cb func() error)
```

Use `NoLogDefer` when a cleanup operation's error can be **silently discarded** — for example, rolling back a database transaction (which is a no-op if the transaction already committed) or closing an HTTP response body.

<good-example>

```go
dbtx, err := s.repo.DB().Begin(ctx)
if err != nil {
    return nil, oops.E(oops.CodeUnexpected, err, "error accessing resource").LogError(ctx, logger)
}
defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
```

</good-example>

<good-example>

```go
defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
```

</good-example>

<important>

- ALWAYS use `o11y.LogDefer` or `o11y.NoLogDefer` for deferred cleanup instead of bare `defer resource.Close()` calls. Bare defers silently discard errors with no traceability.
- Choose `LogDefer` when the error matters for debugging (file I/O, critical resource cleanup).
- Choose `NoLogDefer` when the error is expected or inconsequential (transaction rollbacks, response body closes).

</important>

## Testing

- When writing assertions, use `github.com/stretchr/testify/require` exclusively.
- Avoid using `time.Sleep` to wait for eventual consistency or async state in tests.It is reported by the `forbidigo` rule `GG013` (enforced repo-wide, with a small grandfathered allowlist in `server/.golangci.yaml`). Poll instead: `require.EventuallyWithT` to wait until assertions pass or `require.Never` to assert a condition never becomes true. Inside an `EventuallyWithT` closure, make assertions with `assert.*` against the supplied `*assert.CollectT` — the one sanctioned use of `assert` over `require`.
- Prefer `testing/synctest` (`synctest.Test` + `synctest.Wait`) for testing purely in-process timer/debounce logic. This is one of the few allowed `time.Sleep` use cases in tests since it is required for advancing the fake clock inside a synctest bubble.
- In tests, use `t.Context()` instead of `context.Background()`, except inside `t.Cleanup(func())` callbacks.
- IMPORTANT: avoid using `t.Run` to create subtests. Prefer writing separate test functions instead.
- All test setup which includes spinning up databases, caches and background workers must go in `setup_test.go` files. Look for these across the codebase for inspiration and guidance.
- NEVER write raw SQL in tests for any Postgres operation — `SELECT`, `INSERT`, `UPDATE`, `DELETE`, transactions (`Begin`/`BeginTx`), `CopyFrom`, and `SendBatch` are all covered. Use SQLc-generated methods. **Default to adding new fixture queries in the relevant domain package's own `queries.sql`** (e.g. a `toolsets`-shaped fixture goes in `server/internal/toolsets/queries.sql`, not in `testenv`). Reach for `server/internal/testenv/queries.sql` (and `testenv/testrepo`) only when a fixture query is genuinely reused across multiple packages. The `glint` `no-testing-raw-sql` rule enforces this against `*pgxpool.Pool`, `*pgx.Conn`, `pgx.Tx`, and `pgx.Querier` receivers in `*_test.go`. ClickHouse uses a different driver and is not flagged.
- Use `github.com/stretchr/testify/mock` for mocking third-party libraries in tests instead of ad hoc fakes around vendor types.
- Use `testenv.NewLogger(t)`, `testenv.NewTracerProvider(t)`, and `testenv.NewMeterProvider(t)` instead of constructing loggers or noop OTel providers inline. `testenv.NewLogger(t)` discards in normal runs and emits pretty logs under `go test -v`, which inline `slog.New(slog.DiscardHandler)` and `slog.New(slog.NewTextHandler(os.Stdout, nil))` do not. Exception: tests that assert on log output should use a capturing handler over a `bytes.Buffer`.

<bad-example>

```go
ctx := context.Background()
```

This loses the test lifecycle context that Go now provides directly on `*testing.T`.

</bad-example>

<good-example>

```go
ctx := t.Context()
```

</good-example>

<good-example>

```go
type mockEmailClient struct {
    mock.Mock
}

func (m *mockEmailClient) Send(ctx context.Context, message Message) error {
    args := m.Called(ctx, message)
    return args.Error(0)
}
```

Use `testify/mock` when mocking integrations so expectations stay explicit and consistent across tests.

</good-example>
