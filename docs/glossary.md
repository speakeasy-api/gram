# Glossary

Shared vocabulary for the Gram codebase. Seeded during the AGE-2879 design
grilling (environments on remote MCP servers); extend as terms are pinned down.

## MCP server (`mcp_servers`)

The user-facing _fronting configuration_ for an MCP endpoint. Selects exactly one
backend — a toolset, a remote MCP server, or a tunneled MCP server (CHECK
`mcp_servers_backend_exclusivity_check`) — and carries the environment
attachment, OAuth/issuer settings, visibility, and variations group. Addressable
via one or more `mcp_endpoints`.

## Remote MCP server (`remote_mcp_servers`)

A _backend_: an upstream MCP endpoint that Gram proxies to. Owns Remote MCP
Headers. Referenced by `mcp_servers.remote_mcp_server_id`.

## Remote MCP header (`remote_mcp_server_headers`)

A header Gram sends to the upstream remote MCP server. `is_required`,
`is_secret`, and holds either a static inline `value` (encrypted) or a
`value_from_request_header`. Per ADR 0002 (revised) a header opts into
environment sourcing by storing an _empty_ static value; at serve time it is
filled by name-match from the server's attached environment. No new column.

## Environment (`environments`)

A project-scoped, shareable bag of key/value Entries where secrets live. Attached
to an MCP server via `mcp_servers.environment_id` (and, on the legacy toolset
path, via `mcp_metadata.default_environment_id`).

## Header Definition (external MCP)

An external-MCP name mapping `{Name: env-var-name, HeaderName: HTTP-header-name}`.
Pure remapping — carries no value. Contrast with a Remote MCP Header, which holds
its own value. See `server/internal/externalmcp/config.go`.

## MCP metadata (`mcp_metadata`)

The toolset-path environment-attachment model: `default_environment_id` plus
per-variable Environment Configs (`providedBy: system | user | none`,
`headerDisplayName`). Drives the `EnvironmentSwitcher` UI. Not used by the remote
backend.

## Attached environment

The environment referenced by `mcp_servers.environment_id`. Per ADR 0001 it must
be consumed at runtime, not merely persisted. Per ADR 0005 it is authoritative —
no `Gram-Environment` request header overrides it on the remote path.

## Inbound vs outbound authentication

Two opposite directions that must not be conflated (ADR 0004):

- **Inbound** — who may connect _to_ a Gram MCP server (user-session issuers,
  identity providers). This is the `x/` page "Authentication" section.
- **Outbound / upstream** — the credentials Gram sends _to_ the remote backend
  (the Remote MCP Headers). This is what AGE-2879 configures.

## Fronting server vs backend

- **Fronting server** — the `mcp_servers` row a client addresses. Holds
  `environment_id`.
- **Backend** — the `remote_mcp_servers` row (with its header rows) that a
  fronting server proxies to. One backend may front multiple servers, so an
  env-sourced header (empty static value, per ADR 0002) is resolved per fronting
  server by name-match against that server's attached environment (ADR 0004).
