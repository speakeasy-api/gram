# ADR 0005: The attached environment is authoritative; no per-request env or value override

- Status: Accepted
- Date: 2026-07-08
- Ticket: [AGE-2879](https://linear.app/speakeasy/issue/AGE-2879/allow-attaching-environments-to-mcp-servers)
- Builds on: ADR 0002

## Context

Two existing mechanisms could let a caller influence environment resolution at
request time:

- The toolset path honors a `Gram-Environment` request header to pick the
  environment by slug at call time (`server/internal/mcp/impl.go`).
- External MCP's `BuildHeaders` lets per-request `userConfig` override env values
  for defined keys.

Remote MCP already models "the value comes from the caller" as a first-class
header source: `value_from_request_header`.

## Decision

- **No `Gram-Environment` override on the remote serve path.** The environment
  attached to the fronting `mcp_server` (`environment_id`) is authoritative.
- **No per-request user-provided value layer** for env-sourced headers. Env-sourced
  headers resolve only from the stored attached environment (system values). When a
  value should come from the caller, model it with `value_from_request_header`
  instead.

## Rationale

- Remote MCP is a pure proxy; per-request environment switching adds a
  secret-selection attack surface with no clear v1 use case.
- Keeps the three header value sources orthogonal: static, from-request-header,
  from-environment-variable. No override precedence rules to reason about.

## Consequences

- The serve path resolves env-sourced headers deterministically from one
  environment, with no request-derived environment selection.
