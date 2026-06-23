# Tunnel POC — local k8s

Spike of the secure-tunnel architecture (WebSocket + yamux) for MCP servers on
outbound-only networks. **Local only. No DB — Redis/in-memory route store.**

## Components

| Piece           | Code                                          | Image                       | Role                                                                                                                                                                 |
| --------------- | --------------------------------------------- | --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Agent           | `tunnel/agent`, `tunnel/cmd/tunnel-agent`     | `gram-tunnel-agent:local`   | Customer-side. Dials gateway WS, runs yamux _server_, reverse-proxies substream HTTP to one pinned local MCP URL. Outbound-only, jittered reconnect.                 |
| Gateway         | `tunnel/gateway`, `tunnel/cmd/tunnel-gateway` | `gram-tunnel-gateway:local` | Terminates agent WS upgrades (`/connect`), owns yamux sessions, maps internal forward requests onto substreams by tunnel ID. In-memory registry + Redis route cache. |
| Gram serve path | `server/internal/tunnels`                     | (in gram-server)            | Route lookup → plain-HTTP forward to the owning gateway pod. tunnelID injected server-side, never from the caller.                                                   |
| Shared          | `tunnel/wire`, `tunnel/route`                 | —                           | WS↔net.Conn adapter, key format, version gate, control frames; route store (memory + redis).                                                                         |

The MCP server is deployed elsewhere — set the agent's `TUNNEL_LOCAL_MCP_URL`
(`20-agent.yaml`) to its URL. `tunnel/cmd/sample-mcp` remains as an OPTIONAL
local echo target (not built/deployed by default; re-add it in
`build-and-load.sh` and apply `00-sample-mcp.yaml` to use one).

## Request path

```
MCP client → gram-server (internal/tunnels.ServeTunnel, tunnelID from project-scoped join)
           → plain HTTP → tunnel-gateway /forward (X-Gram-Tunnel-Id header)
           → yamux substream → agent → local MCP server
```

## Deploy

Only the **gateway** runs in k8s (control-plane side). The **agent is NOT a
cluster workload** — it is the customer-side component and runs standalone next
to the MCP server (outbound-only). See "Run the agent" below.

```bash
# 1. Build + load the gateway image into kind
KIND_CLUSTER=local-mess ./.local-k8s/tunnel/build-and-load.sh

# 2. Apply the gateway
kubectl --context kind-local-mess apply -f .local-k8s/tunnel/10-gateway.yaml
kubectl --context kind-local-mess -n gram-local rollout status deploy/tunnel-gateway
```

Reuses the existing `gram-local` Redis (route cache) and ingress-nginx. The agent
reaches the gateway over the ingress host `tunnel.gram.local` — add
`127.0.0.1 tunnel.gram.local` to `/etc/hosts`.

## Run the agent (standalone, next to your MCP)

The agent dials the gateway and reverse-proxies to one pinned MCP URL. Run it as
a plain executable wherever the MCP server lives — not in the cluster.

```bash
TUNNEL_LOCAL_MCP_URL=http://localhost:<mcp-port> ./.local-k8s/tunnel/run-agent.sh
```

Defaults: gateway `ws://tunnel.gram.local/connect`, the demo seed key from
`10-gateway.yaml`. Override `TUNNEL_GATEWAY_URL` / `TUNNEL_KEY` as needed. Or run
the built image directly:

```bash
docker run --rm \
  -e TUNNEL_GATEWAY_URL=ws://host.docker.internal/connect \
  -e TUNNEL_KEY=gram_tunnel_localpocdemokey000000000000000000000000 \
  -e TUNNEL_LOCAL_MCP_URL=http://host.docker.internal:<mcp-port> \
  gram-tunnel-agent:local
```

## Demo the tunnel (simulates gram-server's forward)

With the agent running (above), the gateway's internal forward endpoint maps a
request onto the agent's substream when given the tunnel-ID header. Port-forward
the gateway and curl it:

```bash
kubectl --context kind-local-mess -n gram-local port-forward svc/tunnel-gateway 8090:8090 &

# initialize round-trips through agent → your MCP and back:
curl -s -XPOST localhost:8090/anything \
  -H 'X-Gram-Tunnel-Id: demo-tunnel' \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize"}'

# unknown tunnel → distinct 502 (tenant isolation), never a leak:
curl -i -s localhost:8090/x -H 'X-Gram-Tunnel-Id: nope' | head -1
```

## Tests

```bash
go test ./tunnel/... ./server/internal/tunnels/...
```

- `tunnel/e2e` — real yamux-over-WS session: connect → forward → echo; tenant-isolation 502; revoke kills the session.
- `tunnel/wire` — WSConn+yamux stability and the http.Serve/http.Client topology.
- `server/internal/tunnels` — gram serve path forwards with the injected tunnel-ID header; no-route 502.

## Scope notes (per the design, deliberately deferred)

- No Postgres `tunnels` table, no Goa `/rpc/tunnels.*` API, no audit/RBAC wiring (Phase 1 items needing the DB-backed serve path).
- `internal/tunnels.ServeTunnel` is a standalone handler; hooking it into `internal/mcp`'s `serveendpoint` + the remotemcp interceptor chain is a one-call-site change left out to keep the POC off the DB path.
- Single gateway replica, in-memory registry (Phase-1 honest MVP). Drain protocol / multi-replica pod-to-pod routing = Phase 2.
- Keys seeded from a Secret (static demo value); real per-tunnel key mint/rotate is the management API.
