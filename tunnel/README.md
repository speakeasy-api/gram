# Gram Secure Tunnel

Reach an MCP server that has no inbound connectivity. The customer's agent runs
next to the MCP server, opens one outbound WebSocket to Gram's tunnel gateway,
and the gateway forwards MCP traffic back over yamux substreams.

## Status

The backend control plane exists:

- `tunnelled_mcp_servers` is the durable Postgres source record.
- `/rpc/tunnelledMcp.*` creates, lists, updates, and deletes tunnelled MCP
  sources, with RBAC and audit logging.
- `mcp_servers.tunnelled_mcp_server_id` links a hosted MCP server to a
  tunnelled MCP source.
- The MCP serve path resolves live tunnel routes from Redis, injects the tunnel
  ID server-side, and reuses the remote MCP proxy stack for auth, usage, tool
  logs, and stream metadata.

The gateway resolves presented tunnel keys against the key hashes stored in
Postgres. Redis is the live routing table and connection snapshot store.

## Pieces

| Piece       | Code                                          | Responsibility                                                                                   |
| ----------- | --------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| Agent       | `tunnel/agent`, `tunnel/cmd/tunnel-agent`     | Customer-side process. Dials the gateway and proxies substream HTTP to one pinned local MCP URL. |
| Gateway     | `tunnel/gateway`, `tunnel/cmd/tunnel-gateway` | Accepts agent WebSockets, owns yamux sessions, and forwards requests by tunnel ID.               |
| MCP serve   | `server/internal/mcp/serveendpoint.go`        | Resolves the tunnel route, injects `X-Gram-Tunnel-Id`, and runs the remote MCP proxy path.       |
| Management  | `server/internal/tunnelledmcp`                | Goa service backed by Postgres plus Redis connection metadata.                                   |
| Shared wire | `tunnel/wire`, `tunnel/route`                 | Key format, control frames, WS `net.Conn`, Redis route and connection stores.                    |

## Request Path

```
MCP client
  -> gram-server /mcp/<slug>
  -> mcp_servers row resolves tunnelled_mcp_server_id
  -> Redis lookup: tunnel_routes:<tunnelID> -> gateway address
  -> gram-server proxies to gateway with X-Gram-Tunnel-Id
  -> gateway opens yamux substream to a live agent
  -> agent proxies to TUNNEL_LOCAL_MCP_URL
  -> customer MCP server
```

The caller never supplies the tunnel ID. Gram derives it from the project-scoped
MCP server row and overwrites any inbound tunnel header before forwarding.

## State

Postgres is durable control-plane state:

- `tunnelled_mcp_servers`: display name, key hash/prefix, lifecycle status,
  persisted agent version, last-seen timestamp, soft delete.
- `mcp_servers.tunnelled_mcp_server_id`: hosted MCP server binding.

Redis is live data-plane state:

- `tunnel_routes:<tunnelID>` -> gateway address, refreshed while an agent is
  connected.
- `tunnel_connections:<tunnelID>` -> live connection snapshots for UI/API
  overview data.

## Local Validation

Run `mise run seed`, then start the normal Gram stack with `madprocs`. The local
Postgres MCP server and agent are declared in `compose.yml` under the `tunnel`
profile:

```bash
docker compose --profile tunnel up --build tunnel-postgres-mcp tunnel-agent
```

Two madprocs entries wrap the same local path:

- `tunnel-gateway`: local gateway on `:8090`, using Redis for routes.
- `tunnel-postgres-mcp`: starts Postgres MCP and the companion tunnel agent.

The seed task writes the local tunnel ID and key to `mise.local.toml`:

```bash
TUNNEL_LOCAL_ID=<tunnelled_mcp_servers.id>
TUNNEL_LOCAL_KEY=<one-time tunnel key>
TUNNEL_LOCAL_MCP_ENDPOINT_SLUG=<mcp endpoint slug>
```

The Compose service runs the agent in the Postgres MCP network namespace and
pins the upstream to `http://127.0.0.1:9000/mcp`; Gram reaches it through the
normal tunnelled MCP endpoint seeded in the dashboard.

## Tests

```bash
go test ./tunnel/... ./server/internal/tunnelledmcp/...
```

`server/internal/mcp` covers the production MCP serve path that routes tunnelled
MCP servers through the remote MCP proxy stack.
