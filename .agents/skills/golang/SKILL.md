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
- When using a slog logger, always use the context-aware methods: `DebugContext`, `InfoContext`, `WarnContext`, `ErrorContext`.
- When logging errors make sure to always include them in the log payload using `slog.String("error", err)`. Example: `logger.ErrorContext(ctx, "failed to write to database", slog.String("error", err))`.
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
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
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
  logger.Error("failed to create user", attr.SlogError("error", err))
}
```

This is bad because it uses `logger.Error` instead of `logger.ErrorContext`.

</bad-example>

<good-example>

```go
import "github.com/speakeasy-api/gram/functions/internal/attr"

func Example(ctx context.Context) {
  logger.ErrorContext(ctx, "failed to create user", attr.SlogError("error", err))
}
```

This is great because:

- It uses `logger.ErrorContext` which is the convention for logging in the server codebase.
- It uses the `attr.SlogError` attribute from the attr package.

</good-example>

## Conversion utilities (`server/internal/conv`)

Use the `conv` package for common type conversions instead of writing inline helpers. Key functions:

- `conv.Ptr(v)` — create a pointer to a literal value or return value. Use this instead of declaring a variable just to take its address.
- `conv.PtrEmpty(v)` — like `Ptr` but returns `nil` when `v` is the zero value.
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
    return nil, oops.E(oops.CodeUnexpected, err, "error accessing resource").Log(ctx, logger)
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
- IMPORTANT: avoid using `t.Run` to create subtests. Prefer writing separate test functions instead.
- All test setup which includes spinning up databases, caches and background workers must go in `setup_test.go` files. Look for these across the codebase for inspiration and guidance.
- NEVER write bare SQL queries in tests to insert test data. Always use SQLc queries and create ones to support testing if necessary. Although more preferrably orchestrate the various services to create the necessary state for testing.
