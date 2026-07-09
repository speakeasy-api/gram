# ADR 0004: Environment UI splits by entity — attachment on the fronting page, header sources on the source page

- Status: Accepted
- Date: 2026-07-08
- Ticket: [AGE-2879](https://linear.app/speakeasy/issue/AGE-2879/allow-attaching-environments-to-mcp-servers)
- Builds on: ADR 0002

## Context

The data spans two entities on two dashboard pages:

- `mcp_servers.environment_id` (the _fronting_ server) is edited on the new
  `client/dashboard/src/pages/mcp/x/` page.
- `remote_mcp_server_headers` (the _backend_) are edited on the source page,
  `client/dashboard/src/pages/sources/remote-mcp/RemoteMCPDetails.tsx`.

Because one backend can front multiple `mcp_servers`, a header's env-sourcing
(per ADR-0002, an opt-in header with an empty static value) is resolved per
fronting server against whatever environment that server has attached. The header
row lives on the backend; the environment lives on the fronting server;
resolution happens at serve time by name-match.

The existing "Authentication" section on the `x/` page governs the **inbound**
direction (identity providers / user-session issuers for clients connecting _to_
the server). Upstream credentials are the **outbound** direction; conflating them
under "Authentication" is rejected.

## Decision

Split the UI along the entity boundary:

- **Attach/detach an environment** to the server (v1 UI scope): on the `x/` page
  (edits `mcp_servers.environment_id` via `updateMcpServer`).
- **Header env-sourcing opt-in** (leaving a header's static value empty so it is
  filled from the environment by name-match): configured via API/catalog/seed in
  v1. The source page (`RemoteMCPDetails.tsx`) has no per-header editor today, so
  no header-source picker UI ships in v1; if/when a header editor is added it
  belongs on the source page where the rows live.

Do **not** reuse `EnvironmentSwitcher` — it is coupled to the toolset/`mcp_metadata`
model (`mcp_environment_configs`, `mcpMetadataSet` keyed on `toolsetSlug`), none
of which applies to a remote-backed `mcp_server`.

## Consequences

- v1 ships only the attach/detach UI; header opt-in is data set outside the
  dashboard (API/catalog/seed).
- No cross-entity edit from a single page; each page edits the table it owns.
- A future header editor on the source page could expose the opt-in explicitly,
  but v1 keeps the boundary clean.
