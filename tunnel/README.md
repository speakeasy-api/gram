# Speakeasy Secure Tunnel

Reach an MCP server that has no inbound connectivity. The customer's agent runs
next to the MCP server, opens one outbound WebSocket to Speakeasy's tunnel gateway,
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

With an RSA signing key and public-key bundle configured, Speakeasy can send a
signed `X-Speakeasy-Identity` caller assertion through private tunnels. Your server
verifies it against the public JWKS and applies its own access policy. The
[signed caller identity guide](../docs/tunnel-identity.md) covers the issuer,
claims, verification and rotation.

## Pieces

| Piece       | Code                                          | Responsibility                                                                                                                     |
| ----------- | --------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| Agent       | `tunnel/agent`, `tunnel/cmd/tunnel-agent`     | Customer-side process. Dials the gateway and serves substream HTTP from one pinned local MCP URL or stdio MCP server command.      |
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
  -> agent proxies to TUNNEL_LOCAL_MCP_URL, or bridges to TUNNEL_LOCAL_MCP_COMMAND
  -> customer MCP server
```

The caller never supplies the tunnel ID. Speakeasy derives it from the project-scoped
MCP server row and overwrites any inbound tunnel header before forwarding.

## Stdio MCP Servers

Set `TUNNEL_LOCAL_MCP_COMMAND` instead of `TUNNEL_LOCAL_MCP_URL` to serve an
MCP server that speaks the stdio transport. The agent runs the command with
`/bin/sh -c` and bridges Streamable HTTP onto it:

- Each MCP session gets its own server process, started by `initialize` and
  addressed by an agent-minted `Mcp-Session-Id`.
- Messages are relayed verbatim. Server-initiated requests and notifications go
  to the session's GET stream when one is open, otherwise to an open POST
  stream, otherwise to a bounded backlog drained by the next GET stream.
- DELETE, `TUNNEL_STDIO_IDLE_TIMEOUT` (default `30m`), and agent shutdown stop
  the process: stdin closes, then SIGTERM, then SIGKILL to its process group.
- `TUNNEL_STDIO_MAX_SESSIONS` (default `16`) caps concurrent processes; an
  `initialize` past the cap gets HTTP 503.
- The process inherits the agent's environment minus every `TUNNEL_*`
  variable, so credentials for the server go in the agent's environment.
- Processes outlive gateway reconnects, but sessions are local to one agent.
  Run a single agent replica per stdio tunnel.

Only MCP traffic at the root path is served. OAuth back-channel paths return
404, so a stdio tunnel cannot back an OAuth issuer.

The published image contains no language runtimes. To run a Node or Python
server, build on top of it and add the runtime the command needs.

## OAuth Back-Channel Requests

An issuer bound to a tunnel uses the same agent for persisted metadata refresh,
dynamic client registration, token exchange, refresh, and revocation. Speakeasy
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

For a stdio server, swap the URL for a command:

```bash
TUNNEL_GATEWAY_URL=ws://localhost:8090/connect \
TUNNEL_KEY=<one-time tunnel key> \
TUNNEL_LOCAL_MCP_COMMAND='npx -y @modelcontextprotocol/server-everything' \
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
