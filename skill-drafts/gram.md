---
name: gram
description: Top-level orientation for the Gram codebase — what Gram is (an AI control plane for governing MCP authorization policy, enabling AI usage within organizations, and getting observability over that usage), how the major components fit together, and how an MCP client request flows end-to-end. Activate at the start of any Gram task to get your bearings before diving in, especially when work crosses a single component boundary or touches the MCP request/dispatch path.
metadata:
  relevant_files:
    - "CLAUDE.md"
    - "mise.toml"
    - "mprocs.yaml"
    - "server/cmd/gram/start.go"
    - "server/internal/mcp/**"
    - "server/internal/gateway/**"
    - "server/internal/toolsets/**"
---

Gram is an **AI control plane**. It exists to govern MCP authorization policy, enable AI usage inside organizations, and provide observability over that usage. Three pillars: who gets to do what, what they're allowed to reach, and how the org sees what happened.

It does this by letting users build, host, and serve MCP (Model Context Protocol) servers backed by collections of HTTP API tools, Gram Functions, and external MCP passthroughs. Users author **deployments** (uploaded OpenAPI specs, function bundles, or external MCP references), group them into **toolsets**, and Gram exposes each toolset as an MCP server reachable at `/mcp/<slug>` for AI agents to consume. The dashboard, CLI, and SDK manage that lifecycle through a Goa-designed HTTP-RPC management API.

## What it's for

- **Authorization policy for MCP** — a single place to declare which MCP servers exist, who can call them, and which credentials they're allowed to carry. RBAC, API keys, OAuth proxies, and per-toolset access rules originate here.
- **AI usage enablement** — surfacing approved tools to AI agents through MCP-compatible endpoints rather than letting individual teams hand-roll integrations. Includes hosted Gram Functions for custom logic and proxied access to third-party MCP servers.
- **Observability and audit** — every tool call is logged with principal, toolset, and outcome — surfaced via dashboards, queryable for incident review and usage analytics.
- **Self-serve onboarding** of internal APIs and SaaS connectors via the dashboard, CLI, or upload pipeline — without writing custom MCP server code each time.

## Architecture at a glance

| Directory                 | Owns                                                                                                                                                                               |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `server/`                 | Backend: management API (Goa), MCP serving endpoints, Temporal worker, Postgres + ClickHouse + Redis access, RBAC, audit, OAuth, billing. Entry point: `server/cmd/gram/start.go`. |
| `client/dashboard/`       | React/Vite app for the dashboard. Consumes the generated TypeScript SDK in `client/sdk/`.                                                                                          |
| `client/sdk/`             | Generated TypeScript SDK (Speakeasy) and React Query hooks over the management API.                                                                                                |
| `client/landing/`         | Marketing site.                                                                                                                                                                    |
| `elements/`               | React component library / chat interface that talks to Gram MCP servers.                                                                                                           |
| `cli/`                    | Public Gram CLI; wraps the same management API.                                                                                                                                    |
| `functions/`              | OCI runner images and the `gram-runner` Go binary that executes user-deployed Gram Functions on Fly.io or locally.                                                                 |
| `ts-framework/functions/` | TypeScript SDK for function authors (`Gram.tool()` API, manifest generation, MCP passthrough helpers).                                                                             |
| `mock-speakeasy-idp/`     | Mock identity provider used by `madprocs` for local dev.                                                                                                                           |

Inside `server/internal/`, every feature lives in a flat package — `mcp`, `remotemcp`, `toolsets`, `tools`, `deployments`, `oauth`, `auth`, `access`, `authz`, `audit`, `gateway`, `functions`, `instances`, `platformtools`, `chat`, `chatsessions`, `templates`, `triggers`, `customdomains`, `keys`, `usage`, `billing`, etc. — each typically with `impl.go`, `queries.sql`, a `repo/` SQLc package, and tests. The Goa designs that author the management API live alongside under `server/design/`. See `gram-management-api` for the per-service conventions.

## The MCP pathway

When an MCP client (Claude Desktop, Cursor, Elements chat, etc.) talks to a Gram-hosted MCP server, the request flows like this. All paths are under `server/internal/`.

1. **HTTP entry.** `mcp.Attach` (in `mcp/impl.go`) registers four routes on the goa muxer: `POST /mcp/{mcpSlug}` (the JSON-RPC channel), `GET /mcp/{mcpSlug}` (probe / install page), `GET /mcp/{mcpSlug}/install`, and the two `/.well-known/oauth-*` discovery endpoints. The entry point for actual JSON-RPC traffic is `Service.ServePublic` in `mcp/impl.go`.
2. **Toolset resolution.** `loadToolsetFromMcpSlug` resolves the slug to a `toolsets_repo.Toolset` (with optional `customdomains.Context`). Toolset owns the list of tool URNs, the environment binding, and its OAuth configuration (external OAuth server or OAuth proxy provider).
3. **Authentication.** `ServePublic` extracts the bearer token and `Gram-Chat-Session` JWT, then branches on whether the toolset is public, has external OAuth, has a Gram OAuth proxy, or is private. `authenticateToken` validates the token via `oauth.Service` or `chatsessions.Manager`. `checkToolsetSecurity` decides whether to 401 with a `WWW-Authenticate` header pointing at `/.well-known/oauth-protected-resource/...`.
4. **Per-method dispatch.** `Service.handleRequest` parses the JSON-RPC envelope (see `mcp/rpc.go`) and routes by method name. Implementations live in `mcp/rpc_initialize.go`, `rpc_ping.go`, `rpc_prompts_list.go`, `rpc_prompt_get.go`, `rpc_resources_list.go`, `rpc_resources_read.go`, `rpc_tools_list.go`, and `rpc_tools_call.go`.
5. **Tool dispatch.** `handleToolsCall` (in `rpc_tools_call.go`) loads the tool definition and constructs a `gateway.ToolCallPlan`, then calls `gateway.ToolProxy.Do` (in `gateway/proxy.go`). `ToolProxy` switches on `plan.Kind`:
   - **HTTP-API tool** → `doHTTP`: builds the upstream HTTP request from the OpenAPI-derived plan, evaluates env vars and secrets via `toolconfig.ToolCallEnv`, runs the call through `guardian.Policy` egress controls, and streams the response back.
   - **Gram Function tool** → `doFunction`: hands off to `functions.ToolCaller` (Fly.io runner or local runner — see `gram-functions`).
   - **External MCP passthrough** → `doExternalMCP`: reverse-proxies to the upstream MCP server (`externalmcp/`).
   - **Platform tool** → `doPlatform`: invokes a built-in tool from `platformtools/` (Slack, triggers, logs, etc.).
   - **Prompt tool** → `doPrompt`.
6. **Response path.** Tool output is wrapped back into a JSON-RPC response. The `mcp-passthrough` meta tag (`mcp/passthrough.go`) bypasses extra formatting when the upstream already returned MCP-shaped content. Billing usage is recorded via `billing.Tracker`; tool-call telemetry is logged via `telemetry.Logger`.

Adjacent surfaces:

- **Remote MCP servers** (`remotemcp/`) — the management API for _registering_ an external MCP server as a Gram-hosted resource. Different from `externalmcp/`, which is the runtime passthrough.
- **MCP metadata** (`mcpmetadata/`) — install-page rendering, OAuth client registration metadata, and connector descriptors.
- **Instances** (`instances/`) — single-tool execution endpoint used by Elements chat, separate from the JSON-RPC MCP surface.

## Where to look for X

- Designing or modifying a `/rpc/<service>.<method>` endpoint, including OpenAPI/SDK regeneration → `gram-management-api`.
- Authoring a Gram Function or changing the runner → `gram-functions`.
- Adding scopes, gating handlers, or wiring `<RequireScope>` in the dashboard → `gram-rbac`.
- Recording or surfacing audit events → `gram-audit-logging`.
- Writing Go code in `server/` → `golang`.
- SQLc queries, migrations, schema changes → `postgresql`.
- ClickHouse analytics queries → `clickhouse`.
- Dashboard or Elements React work → `frontend`, plus `vercel-react-best-practices` for performance.
- Running and inspecting traces during local dev → `jaeger`.
- Production observability → `datadog`, `datadog-insights`.
- Writing or editing `.mise-tasks/` scripts → `mise-tasks`.
- Current Gram OAuth surface and the in-flight rewrite → `gram-legacy-oauth`.
- The internal MCP server / backend split (`mcp_servers`, `mcp_endpoints`, backends) → `mcp-servers-and-endpoints`.

## Local development

`madprocs` (configured in `mprocs.yaml`) launches the full stack — `mock-idp`, `server`, `worker`, `dashboard`, `elements` — in a single tabbed TUI. From the CLI: `madprocs status|logs|start|stop|restart <proc>`.

Key mise tasks:

- `mise run start:server --dev-single-process` — run server and Temporal worker in one process (`GRAM_SINGLE_PROCESS=1`).
- `mise run build:server` / `mise run lint:server` / `mise run test:server`.
- `mise run gen:server` — regenerate SQLc + Goa server stubs.
- `mise run gen:sdk` — regenerate the TypeScript SDK and OpenAPI outputs after a Goa design change.
- `mise tasks` to discover the rest; `mise run <task> --help` for arguments.

Defaults and env vars live in `mise.toml`; local overrides go in `mise.local.toml` (gitignored). New developers should run `./zero --agent` to bootstrap.
