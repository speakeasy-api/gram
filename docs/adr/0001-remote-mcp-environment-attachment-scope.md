# ADR 0001: AGE-2879 delivers a full vertical slice for environments on remote MCP servers

- Status: Accepted
- Date: 2026-07-08
- Ticket: [AGE-2879](https://linear.app/speakeasy/issue/AGE-2879/allow-attaching-environments-to-mcp-servers)
- Milestone: Make Remote MCP the Default Target for Catalog Servers

## Context

`mcp_servers.environment_id` already exists (nullable FK, `ON DELETE SET NULL`),
the `mcpServers` management API already accepts and persists it, and
`parseServerIDs` already validates that the environment belongs to the caller's
project. However, **no runtime path reads it**: `serveRemoteBackend`
(`server/internal/mcp/serveendpoint.go`) goes straight from
`remote_mcp_server_headers` rows to `ProxyManager.Build` and never loads an
environment. An attached environment is therefore inert for a remote-backed
server today.

The toolset-backed path is the only place environments are usable, via
`mcp_metadata.default_environment_id` + `mcp_environment_configs`, surfaced by
the `EnvironmentSwitcher` component on the legacy toolset MCP page. Remote-backed
`mcp_servers` use the newer `client/dashboard/src/pages/mcp/x/` page, which has
no environment section.

## Decision

AGE-2879 ships a **full vertical slice**: the dashboard lets a user attach/detach
an environment to a remote-backed `mcp_server`, AND the remote-proxy serve path
resolves upstream header values from the attached environment at request time.
Persisting the association without consuming it is explicitly rejected — dead
config is a trap.

## Consequences

- Runtime work lands in the remote serve path (`serveRemoteBackend` →
  `ProxyManager.Build`), not just the API/UI.
- Tunneled servers share the remote proxy stack; their treatment is decided
  separately (see forthcoming ADR).
- Pairs with AGE-2881 (default the Catalog "Add to Project" flow to a Remote MCP
  target); together they make Remote MCP the catalog default.
