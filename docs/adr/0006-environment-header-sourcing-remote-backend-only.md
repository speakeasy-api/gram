# ADR 0006: Environment-sourced headers are remote-backend-only for v1

- Status: Accepted
- Date: 2026-07-08
- Ticket: [AGE-2879](https://linear.app/speakeasy/issue/AGE-2879/allow-attaching-environments-to-mcp-servers)
- Builds on: ADR 0002

## Context

`mcp_servers.environment_id` exists on every server regardless of backend
(toolset / remote / tunneled). The environment-sourced header mechanism (ADR 0002)
binds an env var to a `remote_mcp_server_headers` row.

Tunneled servers share the remote proxy stack (`ProxyManager.BuildTarget`) but
send only Gram-injected routing headers (`tunnelrouting.Headers`); they have **no
user-configurable upstream header rows**. There is nothing for an env var to bind
to on the tunneled path. Toolset-backed servers already have their own
environment model (`mcp_metadata` + `EnvironmentSwitcher`).

## Decision

AGE-2879's environment-sourced header values apply to **remote-backed servers
only**. On tunneled servers `environment_id` remains inert/unsupported for header
sourcing until a tunneled upstream-header mechanism is designed. The toolset path
is unchanged.

## Consequences

- Matches the milestone: Remote MCP is the catalog default; catalog servers are
  remote-backed.
- A future ticket owns tunneled upstream-header configuration if a use case
  emerges.
