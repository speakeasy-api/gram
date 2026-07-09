# ADR 0002: Remote MCP headers source from the attached environment by name-match (zero schema change)

- Status: Accepted (revised 2026-07-08; supersedes the original "explicit column" decision below)
- Date: 2026-07-08
- Ticket: [AGE-2879](https://linear.app/speakeasy/issue/AGE-2879/allow-attaching-environments-to-mcp-servers)

## Context

Two structurally opposite header models exist:

- **External MCP** treats headers as _projections of the environment_. Header
  Definitions (`{Name: env-var-name, HeaderName: HTTP-header-name}`) are pure
  name mappings; the attached environment is the value source. `BuildHeaders`
  (`server/internal/externalmcp/config.go`) projects env entries to headers,
  deriving the HTTP header name via `toolconfig.ToHTTPHeader` when no definition
  overrides it. External MCP headers store no static values.
- **Remote MCP** headers (`remote_mcp_server_headers`) are _self-contained_: each
  row holds its own static encrypted `value` OR a `value_from_request_header`,
  enforced by CHECK `(value IS NULL) != (value_from_request_header IS NULL)`
  (exactly one of two). The environment is never involved.

Attaching an environment to a remote server introduces a value source those
headers never had. The models collide; one has to win.

## Decision

Consume the attached environment **at serve time via name-match, with no schema
change**:

- A remote MCP header opts into environment sourcing by being stored with an
  **empty static value** (`value = ''`). The existing CHECK permits this
  (`value TEXT` has no non-empty constraint, unlike `value_from_request_header`),
  so no migration is required.
- At serve time `serveRemoteBackend` loads the attached environment (when
  `mcp_servers.environment_id` is set) and, for each opt-in header, fills its
  value from the matching environment entry. The match derives the HTTP header
  name from each env entry via `toolconfig.ToHTTPHeader` and compares to the
  header's `Name` (case-insensitive), mirroring external MCP's convention.
- **Only configured headers are filled.** The environment is never
  wholesale-projected, so unrelated env entries never reach the upstream.

## Rationale

- Zero schema change: no new column, no CHECK rewrite, no Goa/query edits. The
  `mcp_servers.environment_id` association already exists; this ADR only adds the
  runtime glue.
- Per-header opt-in preserved via the empty-value marker: only headers a
  configurator explicitly leaves empty pull from the environment.
- No wholesale-injection leak: matching against _configured_ headers bounds
  exactly what is sent upstream (see ADR-0005).
- Reuses the proven `toolconfig.ToHTTPHeader` derivation already used by external
  MCP, rather than inventing a new mapping.
- Existing static and request-header semantics are untouched; a header with a
  real static value always wins over the environment.

## Rejected alternatives

- **Explicit `value_from_environment_variable` column (original decision).** A
  third per-header source with a `num_nonnulls(...) = 1` CHECK. Most auditable
  (the reference is data on the row), but requires a migration plus query, Goa,
  impl, and model-view changes. Dropped: the schema cost is not justified when
  name-match delivers the same controlled per-header behavior for free.
- **Literal wholesale injection.** Turn every attached-env entry into an upstream
  header. Truly zero-schema but sends the entire (shared) environment to a
  third-party upstream on every request — a secret-leak vector. Rejected on
  security grounds.

## Consequences

- No migration. Serve path loads the attached environment when
  `environment_id` is set and fills opt-in headers before proxying.
- The opt-in signal (empty static value) is implicit; acceptable for v1 where
  headers are configured via API/catalog/seed. May be made more explicit later.
- Match semantics (derive-via-`ToHTTPHeader` vs direct `Name` lookup) is an
  implementation nuance tracked in the plan's unresolved questions.
- Missing/empty resolution for required headers is covered by ADR-0003
  (fail-closed via the existing `ConfiguredHeader.Resolve`).
