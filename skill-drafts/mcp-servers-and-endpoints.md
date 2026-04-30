---
name: mcp-servers-and-endpoints
description: Internal-only Gram architectural concept that separates the MCP "server" (the public-facing surface MCP clients connect to, configured by an `mcp_servers` row and addressed by `mcp_endpoints` rows) from the MCP "backend" (the `toolsets` row or `remote_mcp_servers` row the server points at). Activate whenever the task touches MCP request routing, endpoint resolution, OAuth wiring on MCP servers, or anything that has to decide "is this concern about the surface a client connects to, or about the backend that fulfills the call?". This is a Gram-specific internal split — it has nothing to do with anything in the public MCP specification. Note: this terminology was renamed in 2026-04 (was: MCP frontend / MCP slug).
metadata:
  relevant_files:
    - "server/internal/mcp/**/*.go"
    - "server/internal/remotemcp/**/*.go"
    - "server/internal/mcpmetadata/**/*.go"
    - "server/internal/externalmcp/**/*.go"
    - "server/internal/mcpservers/**/*.go"
    - "server/internal/mcpendpoints/**/*.go"
    - "server/design/mcpservers/**"
    - "server/design/mcpendpoints/**"
    - "server/database/schema.sql"
---

Gram splits the MCP server lifecycle into two independent layers. The **MCP server** (table: `mcp_servers`) is the public-facing surface — what an MCP client sees, addresses, and authenticates against. The **MCP backend** is whatever fulfills tool listings and tool calls behind that surface — a Gram toolset (`toolsets` row), or an upstream MCP endpoint Gram proxies to (`remote_mcp_servers` row). The split is internal Gram terminology and is not part of the public MCP spec.

> **Naming history.** Until 2026-04 the surface table was called `mcp_frontends` and its routing rows were `mcp_slugs`. They were renamed to `mcp_servers` and `mcp_endpoints` (the `slug` column is preserved on `mcp_endpoints`). Management API services renamed: `mcpFrontend` → `mcpServer`, `mcpSlug` → `mcpEndpoint`. Audit subjects renamed: `mcp-frontend:*` → `mcp-server:*`, `mcp-slug:*` → `mcp-endpoint:*`.

## What the split is

**MCP server** owns: the URL the client hits, MCP transport (handshake, session, SSE), tool/prompt/resource listing, tool-call dispatch, OAuth metadata advertised at `/.well-known/oauth-protected-resource/...`, custom-domain routing, and (in the OAuth rewrite) client-session issuance. The package is `server/internal/mcp/` and its entry point is `Service.ServePublic` mounted at `POST /mcp/{slug}`.

**MCP backend** owns: the actual content of a server — either a `toolsets` row (Gram-native tools assembled from deployments) or a `remote_mcp_servers` row (an upstream MCP endpoint Gram proxies to). Backends are managed by `server/internal/toolsets/` and `server/internal/remotemcp/`. They have no opinion about how a client reaches them.

**Endpoint** is the routing primitive that joins the two. An `mcp_endpoints` row (with its preserved `slug` column, optionally scoped to a `custom_domain_id`) maps a client request URL to exactly one MCP server, which in turn points at exactly one backend.

## Why it exists

Multiple MCP servers can reuse a single backend, and a backend's lifecycle is independent of the URLs that point at it. This buys: branded MCP URLs over custom domains; multi-tenant slug ownership; the ability to swap backends (toolset → remote MCP server) without breaking client URLs; and — most relevant here — a clean place to attach OAuth configuration that lives at the surface layer rather than inside the toolset or the remote server.

## How it works in code today

The decoupling is **partially landed**. The schema (`mcp_servers`, `mcp_endpoints` tables) is already on `main`. The MCP runtime in `server/internal/mcp/` still resolves slugs through the **legacy** path:

- `ServePublic` reads the slug from the URL path-param.
- `loadToolsetFromMcpSlug` queries `toolsets.mcp_slug` directly. The `toolsets` table itself still owns `mcp_slug`, `mcp_is_public`, `mcp_enabled`, `external_oauth_server_id`, and `oauth_proxy_server_id` columns.
- OAuth wiring is read off the toolset row.

The management-API half landed in PR #2412 (originally titled around the old "MCP frontend" / "MCP slug" naming; under the rename, services live at `server/internal/mcpservers/` and `server/internal/mcpendpoints/` with Goa designs at `server/design/mcpservers/` and `server/design/mcpendpoints/`). Endpoints emit `mcp-server:*` / `mcp-endpoint:*` audit events. The cross-package ownership-helper pattern takes a `pgx.Tx` so a single transaction can validate FKs across `environments`, `oauth`, `remotemcp`, `toolsets`, `customdomains`, `mcpservers`. The MCP runtime in `server/internal/mcp/` does **not** consume `mcp_servers` yet — migrating slug resolution and OAuth wiring off `toolsets` and onto `mcp_servers` is future work.

`mcp_servers` constraints worth knowing: exactly one of `remote_mcp_server_id` / `toolset_id`, at most one of `external_oauth_server_id` / `oauth_proxy_server_id`, and a soft delete cascades to child `mcp_endpoints` in the same transaction.

## How it relates to the OAuth rewrite

The server/backend split is a prerequisite for the OAuth rewrite in `prompt.md` / `spike.md`. Mapping the new vocabulary onto the split:

- `client_session_issuer` is an **MCP-server-level** concern. It owns the relationship with the MCP client and issues Gram-signed access tokens (HS256 JWT, `aud = toolset slug`, `sub` = principal URN per goal #10). It is the natural successor to today's per-toolset OAuth columns and conceptually attaches to the `mcp_servers` row.
- `remote_oauth_issuer` and `remote_oauth_client` are also **MCP-server-level** concerns — they describe how Gram authenticates outbound to upstream services on behalf of the MCP server's principal. Backends consume the resulting credentials but do not own them.
- The injection point for goal #11 — "the `client_session_issuer` package never generates its own session IDs; the MCP handler injects `mcp_session_id`" — lives in the MCP server runtime, in `ServePublic` after parsing the MCP session id from the request and before any client-session-issuing call.
- The MCP server runtime is also where the consent-bypass fix from goal #4 belongs — verifying that the existing session has consented for _this_ `client_session_issuer` to access _all_ of its `remote_session_tokens` before skipping consent.

## Open questions for an agent picking this up

- The MCP runtime currently resolves through `toolsets.mcp_slug`, not `mcp_endpoints.slug`. Migrating it is a follow-up. Until then, the live behavioural split is "MCP-server code in `server/internal/mcp/`, backend code in `server/internal/toolsets/` and `server/internal/remotemcp/`" — the schema-level `mcp_servers` / `mcp_endpoints` tables exist but are unread by the hot path.
- `remote_mcp_server_id` is a valid backend in the schema but is **not yet wired** through `ServePublic`'s legacy slug-loader. Routing an MCP request to a remote backend currently goes through other entry points.

## References

Canonical sources of truth (likely require Notion auth — note that titles may still reflect pre-rename "frontend/slug" terminology):

1. RFC: Gram MCP Frontends and Slugs — https://www.notion.so/RFC-Gram-MCP-Frontends-and-Slugs-342726c497cc800ba609de5cbe5f3d38
2. RFC: Gram Remote MCP Servers — https://www.notion.so/speakeasyapi/RFC-Gram-Remote-MCP-Servers-33c726c497cc8072ac6dc6816f3d264f
3. PR #2412 — https://github.com/speakeasy-api/gram/pull/2412 (introduced what is now `mcpServer` / `mcpEndpoint`; verify current title before quoting)

Code:

- MCP server runtime: `server/internal/mcp/impl.go` (`ServePublic`)
- Backends: `server/internal/toolsets/`, `server/internal/remotemcp/`
- Schema: `server/database/schema.sql` (`toolsets`, `remote_mcp_servers`, `mcp_servers`, `mcp_endpoints`)
- Management APIs: `server/design/mcpservers/`, `server/design/mcpendpoints/`, `server/internal/mcpservers/`, `server/internal/mcpendpoints/`
- OAuth rewrite context: `prompt.md` (goals #4, #10, #11), `spike.md` (§3.5), `schemas/postgres.sql`, `schemas/redis.go`, `schemas/jwt.go`
