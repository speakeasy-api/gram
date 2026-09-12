# Gram Secure Tunnel

Reach an MCP server that has no inbound connectivity. The customer's agent runs
next to the MCP server, opens one outbound WebSocket to Gram's tunnel gateway,
and the gateway forwards MCP traffic back over yamux substreams.

## Status

The backend control plane exists:

- `tunneled_mcp_servers` is the durable Postgres source record.
- `/rpc/tunneledMcp.*` creates, lists, updates, and deletes tunneled MCP
  sources, with RBAC and audit logging.
- `mcp_servers.tunneled_mcp_server_id` links a hosted MCP server to a
  tunneled MCP source.
- The MCP serve path resolves live tunnel routes from Redis, injects the tunnel
  ID server-side, and reuses the remote MCP proxy stack for auth, usage, tool
  logs, and stream metadata.

The gateway resolves presented tunnel keys against the key hashes stored in
Postgres. Redis is the live routing table and connection snapshot store.

## Pieces

| Piece       | Code                                          | Responsibility                                                                                                                     |
| ----------- | --------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| Agent       | `tunnel/agent`, `tunnel/cmd/tunnel-agent`     | Customer-side process. Dials the gateway and proxies substream HTTP to one pinned local MCP URL.                                   |
| Gateway     | `tunnel/gateway`, `tunnel/cmd/tunnel-gateway` | Accepts agent WebSockets on the public listener, owns yamux sessions, and forwards requests by tunnel ID on the internal listener. |
| MCP serve   | `server/internal/mcp/serveendpoint.go`        | Resolves the tunnel route, injects `X-Gram-Tunnel-Id`, and runs the remote MCP proxy path.                                         |
| Management  | `server/internal/tunneledmcp`                 | Goa service backed by Postgres plus Redis connection metadata.                                                                     |
| Shared wire | `tunnel/wire`, `tunnel/route`                 | Key format, control frames, WS `net.Conn`, Redis route and connection stores.                                                      |

## Request Path

```
MCP client
  -> gram-server /mcp/<slug>
  -> mcp_servers row resolves tunneled_mcp_server_id
  -> Redis candidates: tunnel_routes:<tunnelID> -> live gateway forward addresses
  -> gram-server selects one gateway by client affinity or random fallback
  -> gram-server proxies to gateway with X-Gram-Tunnel-Id
  -> gateway opens yamux substream to a live agent
  -> agent proxies to TUNNEL_LOCAL_MCP_URL
  -> customer MCP server
```

The caller never supplies the tunnel ID. Gram derives it from the project-scoped
MCP server row and overwrites any inbound tunnel header before forwarding.

## OAuth Back-Channel Requests

An issuer bound to a tunnel uses the same agent for persisted metadata refresh,
dynamic client registration, token exchange, refresh, and revocation. Gram
preserves each OAuth request's path and query, but the agent delivers the request
to the origin pinned by `TUNNEL_LOCAL_MCP_URL`; the original URL's scheme and
host are not used inside the customer network.

If the MCP server and authorization server run on separate origins, point
`TUNNEL_LOCAL_MCP_URL` at a local reverse proxy that routes their paths to the
appropriate services. A tunnel agent cannot select separate upstream origins
for MCP and OAuth traffic on its own.

## State

Postgres is durable control-plane state:

- `tunneled_mcp_servers`: display name, key hash/prefix, lifecycle status,
  persisted agent version, last-seen timestamp, soft delete.
- `mcp_servers.tunneled_mcp_server_id`: hosted MCP server binding.

Redis is live data-plane state:

- `tunnel_routes:<tunnelID>`: sorted set of live gateway addresses, refreshed
  while an agent is connected.
- `tunnel_connections:<tunnelID>`: owner-scoped live connection snapshots for
  UI/API overview data, merged on read.

## Local Validation

Start the local dev stack with `./zero --agent`. The `tunnel-gateway`
pitchfork daemon runs the local gateway with agent `/connect` on `:8090` and
internal forwarding on `:8091`, using Redis for routes.

To exercise the full path, create a tunneled MCP source in the dashboard and
run the agent against any local MCP server with the one-time key it issues:

```bash
TUNNEL_GATEWAY_URL=ws://localhost:8090/connect \
TUNNEL_KEY=<one-time tunnel key> \
TUNNEL_LOCAL_MCP_URL=<local MCP server url> \
TUNNEL_SERVICE_VERSION=dev \
go run ./tunnel/cmd/tunnel-agent
```

## Tests

```bash
mise exec -- go test ./tunnel/...
mise run test:server ./internal/tunneledmcp/...
```

`server/internal/mcp` covers the production MCP serve path that routes tunneled
MCP servers through the remote MCP proxy stack.
