# Gram Secure Tunnel

Reach an MCP server that has **no inbound connectivity** — it sits on a customer
network, behind a NAT/firewall, and can only dial _out_. The tunnel lets
gram-server proxy MCP traffic to it anyway, over a single outbound WebSocket the
customer's agent holds open.

> **Status: POC.** No database, no Goa `/rpc/tunnels.*` API, no RBAC/audit
> wiring. Keys are seeded from env, routes live in Redis or memory. The
> tenant-isolation invariant is real and enforced; the durable control plane is
> deferred. See [Scope](#scope--deferred) at the bottom.

---

## How it works

Three processes, one shared library.

```
   customer network                 |        gram control plane (k8s)
                                     |
  ┌─────────┐   reverse-proxy   ┌────┴────┐   yamux substream   ┌──────────────┐
  │ local   │ ◀──────────────── │  agent  │ ════════════════════│ tunnel-      │
  │ MCP srv │ ──────────────▶   │ (stand- │  ONE outbound WS    │ gateway      │
  └─────────┘                   │  alone) │ ───────────────────▶│ (/connect)   │
                                └─────────┘                     └──────┬───────┘
                                     |                        plain HTTP│ (X-Gram-
                                     |                       pod-to-pod │  Tunnel-Id)
                                     |                          ┌───────┴───────┐
   MCP client ─────────────────────────────────────────────────│  gram-server  │
                                     |                          │ ServeTunnel() │
                                     |                          └───────────────┘
```

### Roles

| Piece          | Code                                          | Runs where                                 | Job                                                                                                                                         |
| -------------- | --------------------------------------------- | ------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------- |
| **Agent**      | `tunnel/agent`, `tunnel/cmd/tunnel-agent`     | Customer side, standalone (NOT in cluster) | Dials gateway WS, runs yamux **server**, reverse-proxies each substream to one **pinned** local MCP URL. Outbound-only, jittered reconnect. |
| **Gateway**    | `tunnel/gateway`, `tunnel/cmd/tunnel-gateway` | Control plane (k8s)                        | Terminates agent WS upgrades at `/connect`, owns the yamux sessions, maps internal forward requests onto substreams by tunnel ID.           |
| **Serve path** | `server/internal/tunnels`                     | Inside gram-server                         | Looks up the gateway pod holding a tunnel, plain-HTTP forwards the MCP request to it. Injects tunnelID server-side.                         |
| **Shared**     | `tunnel/wire`, `tunnel/route`                 | library                                    | WS↔net.Conn adapter, key format, version gate, control frames; route store (memory/redis).                                                  |

### The connection (who dials whom)

The agent makes the **only** connection — one outbound WebSocket to
`wss://<gateway>/connect`. Over that WS runs **yamux** (stream multiplexing).
Inverted roles:

- **Gateway = yamux client** — it _opens_ substreams.
- **Agent = yamux server** — it _accepts_ substreams and serves HTTP on each.

So all traffic flows the "wrong" way down a connection the customer opened
outbound. Each MCP request becomes one fresh substream (`DisableKeepAlives` on
the transport → a new `session.Open()` per request); the substream is a normal
`net.Conn`, the agent runs `http.Serve` over the whole session, and **every
substream — control frames included — is just HTTP** (control paths are matched
by the `/_tunnel/` prefix). SSE survives because both proxies set
`FlushInterval: -1`.

### The request path

Two shapes: the **intended** full build (via `ServeTunnel`) and the **POC demo**
that ran the end-to-end test (via the existing remote-MCP proxy). The gateway →
agent → MCP half is identical; they differ only in how the request reaches the
gateway and where the tunnel ID comes from.

**Intended (full build):**

```
MCP client
  → gram-server: internal/tunnels.ServeTunnel(w, r, tunnelID)
        tunnelID comes from a project-scoped join on the mcp_servers row —
        NEVER from caller input. Same trust shape as RemoteMcpServerID.
  → route.Lookup(tunnelID) → gateway pod address
  → plain HTTP POST to gateway, header X-Gram-Tunnel-Id: <id>
  → gateway: registry.pick(tunnelID) → a live yamux session
  → session.Open() → substream → agent
  → agent reverse-proxies to its pinned TUNNEL_LOCAL_MCP_URL
  → local MCP server
```

**POC demo (what actually ran — see [section 5](#5-wire-it-through-gram-as-a-remote-mcp-server)):**
`ServeTunnel` is unused. The tunnel is configured as a normal **remote MCP
server** whose URL is the gateway, with a **static configured header** carrying
the tunnel ID:

```
MCP client (Claude)
  → gram-server /mcp/<slug>  → remotemcp proxy
  → forwards to the gateway URL, attaching X-Gram-Tunnel-Id (a configured header)
  → gateway: registry.pick(tunnelID) → substream → agent → local MCP server
```

### Routing across pods (the route store)

A different gram-server pod than the one holding the agent session needs to find
the right gateway. On connect the gateway **publishes** `tunnelID → its own
advertise address` into the route store with a 30s TTL, **refreshes** at half-TTL
while the session lives, and **deletes** on disconnect. gram-server's
`ServeTunnel` does a `Lookup` and forwards to that address.

- `route.Memory` — process-local; fine when gateway + serve path share a process
  or single replica.
- `route.Redis` — `tunnel_routes:<id>` keys. A cache only — losing Redis degrades
  routing, never corrupts it (Postgres is the durable truth in the full build).

### Auth & tenant isolation (the invariants)

- **Key format** `gram_tunnel_<hex>` (`wire.KeyPrefix`). Cheap prefix check
  rejects garbage before any store hit. Only the **SHA-256 hash** is stored;
  plaintext is shown once at mint.
- **Binding comes from the stored row, never the token.** `KeyStore.Resolve`
  maps a presented key → its `tunnelID`. Org/project binding lives on the tunnel
  record, not in the key itself.
- **tunnelID is never caller input.** gram-server injects `X-Gram-Tunnel-Id`
  server-side after a project-scoped join. The gateway strips any inbound copy
  of that header before forwarding. An unknown/spoofed tunnel ID yields a
  distinct `502` with `X-Gram-Tunnel-Error: no-live-session` — a refusal, never
  a cross-tenant leak.
- **Agent pins its upstream.** `TUNNEL_LOCAL_MCP_URL` is hard-set on the agent;
  the control plane cannot redirect it (SSRF mitigation).
- **Version gate.** Agent advertises `X-Gram-Agent-Version`; gateway rejects
  below `MinSupportedAgentVersion` with `426 Upgrade Required`. No auto-update.

### HA & lifecycle

- Multiple agents may present the **same** tunnel key (customer-side HA). The
  gateway registers all and **round-robins** substreams across them — duplicate
  registration is defined behavior.
- `RevokeTunnel` kills every session for a tunnel and clears its route.
- Reconnect uses **full jitter** exponential backoff — an ingress reload severs
  the whole fleet at once, so unjittered retries would stampede. A long-lived
  session resets the backoff.
- Drain (`/_tunnel/drain`) frame tells an agent to reconnect (landing on a
  surviving pod) and let in-flight work finish — multi-replica drain is Phase 2.

---

## Local setup (kind, via `.local-k8s`)

The local Gram stack lives in `.local-k8s/` (kind cluster `local-mess`,
namespace `gram-local`, with Postgres/Redis/Temporal/ClickHouse/ingress-nginx).
The tunnel adds onto it from `.local-k8s/tunnel/`. **Only the gateway runs in the
cluster** — the agent and MCP server are customer-side and run standalone.

For the test, the **remote MCP server and the agent were both run on a separate
Ubuntu machine — an [OrbStack](https://orbstack.dev) Linux VM**, standing in for
a customer host. Because that machine cannot reach the kind cluster directly, the
gateway ingress was **exposed publicly via ngrok**; the agent on the Ubuntu
machine dialed `wss://<ngrok-host>/connect` outbound. The MCP server was just a
local HTTP port on that same machine the agent forwarded to.

### 1. Build + load the gateway image into kind

```bash
KIND_CLUSTER=local-mess ./.local-k8s/tunnel/build-and-load.sh
```

Builds static CGO-free linux binaries (`tunnel-gateway`, `tunnel-agent`) and
`kind load`s them (`imagePullPolicy: Never`). To also build the throwaway echo
MCP, append `sample-mcp:./tunnel/cmd/sample-mcp` to `TARGETS`.

### 2. Apply the gateway

```bash
kubectl --context kind-local-mess apply -f .local-k8s/tunnel/10-gateway.yaml
kubectl --context kind-local-mess -n gram-local rollout status deploy/tunnel-gateway
```

`10-gateway.yaml` gives you: the Deployment (1 replica, in-process registry), a
`tunnel-gateway:8090` Service, an Ingress (with both the `tunnel.gram.local` and
the public ngrok host as rules), and a Secret seeding the route store + demo key.
It reuses the existing `gram-local` Redis as the route cache.

Expose the ingress publicly with **ngrok** — the agent runs on a separate
machine, so the demo dials the public host, **not** a `/etc/hosts` entry. Point
ngrok at the ingress (port 80) on a reserved ngrok domain:

```bash
ngrok http --url=<your-tunnel-svc-host>.ngrok.app 80
```

The ngrok host (`<your-tunnel-svc-host>.ngrok.app`) must match an ingress rule in
`10-gateway.yaml` (the agent sends `Host` verbatim — see the
`dennis-tunnel.ngrok.app` rule for the pattern). The agent then dials
`wss://<your-tunnel-svc-host>.ngrok.app/connect`.

### 3. (Optional) a local MCP target

Either point the agent at a real MCP server you're running, or deploy the echo
stub:

```bash
kubectl --context kind-local-mess apply -f .local-k8s/tunnel/00-sample-mcp.yaml
```

`tunnel/cmd/sample-mcp` answers JSON-RPC `initialize` / `tools/list` /
`tools/call` (an `echo` tool) plus an `/sse` endpoint to prove streaming
round-trips.

### 4. Run the agent standalone (next to the MCP)

The agent runs on the customer machine — in the test, a separate Ubuntu host
(OrbStack Linux VM) — and dials the gateway over the **public ngrok host**:

```bash
TUNNEL_GATEWAY_URL=wss://<your-ngrok-host>/connect \
TUNNEL_KEY=gram_tunnel_localpocdemokey000000000000000000000000 \
TUNNEL_LOCAL_MCP_URL=http://localhost:9000 \
./.local-k8s/tunnel/run-agent.sh
```

`run-agent.sh` only requires `TUNNEL_LOCAL_MCP_URL`; it defaults `TUNNEL_KEY` to
the demo seed key from `10-gateway.yaml`. Override `TUNNEL_GATEWAY_URL` with your
ngrok host as above (its `ws://tunnel.gram.local/connect` default is only for an
agent on the same host as the cluster, which is **not** how the demo runs).

### 5. Wire it through Gram as a remote MCP server

The POC does **not** use `internal/tunnels.ServeTunnel` (that dedicated serve
path is deferred — see [Scope](#scope--deferred)). Instead the demo reuses
Gram's existing **remote MCP server** proxy plus the **configured-headers**
feature to inject the tunnel ID. This is how the end-to-end test ran, with a
**sample dummy MCP server** behind the agent:

1. In the Gram dashboard's **remote MCP server setup** (Create Remote MCP /
   server details → Proxy Headers), add a source whose **URL is the tunnel
   gateway's public ngrok host** (the gateway's `/` catch-all forward handler),
   e.g. `https://<your-ngrok-host>/`.
2. In that same setup screen, add a **static proxy header** via the headers
   editor:

   ```
   X-Gram-Tunnel-Id: demo-tunnel
   ```

   `demo-tunnel` is the tunnel ID from the seed key in `10-gateway.yaml`.

3. Point an MCP client (the test used the **Claude desktop client**) at that
   source's Gram MCP URL — `http://gram.local/mcp/<slug>`.

Request flow:

```
Claude client
  → http://gram.local/mcp/<slug>                         (gram-server)
  → remotemcp proxy: forwards to the source URL (ngrok gateway host),
    applying the configured static header X-Gram-Tunnel-Id: demo-tunnel
  → tunnel-gateway handleForward: reads the header, picks the agent session
  → yamux substream → agent → local sample dummy MCP server
```

The gateway keys entirely off `X-Gram-Tunnel-Id`. An unknown ID yields a
distinct `502` (`X-Gram-Tunnel-Error: no-live-session`) — tenant isolation, never
a cross-tunnel leak.

> **Why this works without `ServeTunnel`.** The gateway's forward handler only
> needs the tunnel-ID header and a route to the agent. Gram's remote-MCP proxy
> can already forward to an arbitrary URL and attach configured headers, so
> pointing it at the gateway and setting the header by hand reproduces what
> `ServeTunnel` will eventually do server-side (deriving the ID from a
> project-scoped join instead of a static header).

---

## Remote k8s setup

Same shape, with real ingress and real config instead of the local shortcuts.

**Gateway (cluster side):**

1. Build/push `tunnel-gateway` to your registry; deploy like `10-gateway.yaml`
   but with a real image + a normal `imagePullPolicy`.
2. **`TUNNEL_GATEWAY_ADVERTISE_ADDR`** — the in-cluster address gram-server
   forwards to. Use the Service DNS (`tunnel-gateway.<ns>.svc.cluster.local:8090`)
   for a single replica. For multi-replica, publish the **pod IP** via the
   downward API so the route store points at the exact pod holding the session
   (the `Memory` store won't do — use `TUNNEL_REDIS_ADDR`).
3. **Ingress must be WebSocket-friendly** — long read/send timeouts
   (`proxy-read-timeout: "3600"`) so idle tunnels survive. Terminate TLS so
   agents dial `wss://`.
4. The agent presents its `Host` header verbatim, so **every external hostname
   it might dial needs its own ingress rule** (see the ngrok rule in
   `10-gateway.yaml` for the pattern).

**Agent (customer side):** ships as a standalone binary/container the customer
runs next to their MCP server. Only needs outbound 443 to the gateway host. Set
`TUNNEL_GATEWAY_URL=wss://<your-gateway-host>/connect`, `TUNNEL_KEY`,
`TUNNEL_LOCAL_MCP_URL`.

**Exposing a public gateway for an external agent.** For the test, the kind
gateway was exposed publicly via **ngrok** so an agent on a **separate Ubuntu
machine** (an OrbStack Linux VM, outside the cluster) could dial
`wss://dennis-tunnel.ngrok.app/connect` outbound. Because the agent sends that
Host verbatim, `10-gateway.yaml` carries a second ingress rule for the ngrok
host. In a real remote cluster this is just your normal external ingress
hostname.

---

## Local shortcuts (POC-only — do not ship)

These exist so the POC needs no database or control-plane API. Each maps to a
real subsystem in the full build:

| Shortcut                            | What it does                                                                                                                                                                                   | Real version                                                                                                                  |
| ----------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| **Static seed key**                 | `TUNNEL_SEED_KEYS="demo-tunnel=gram_tunnel_localpocdemokey..."` seeds one tunnelID→key pair into the in-memory `KeyStore`. The plaintext is a fixed demo value checked into `10-gateway.yaml`. | Per-tunnel mint/rotate via the management API; only the hash stored in the `tunnels` table.                                   |
| **Tunnel ID via configured header** | Instead of `ServeTunnel`, the demo points a remote MCP server at the gateway and sets `X-Gram-Tunnel-Id: demo-tunnel` as a **static configured header** (the new headers editor).              | `ServeTunnel` derives tunnelID from a **project-scoped join** on the mcp_servers row and injects it; callers never supply it. |
| **In-memory / Redis route store**   | No durable record of which pod owns a tunnel.                                                                                                                                                  | Postgres is the durable truth; Redis stays as a TTL cache.                                                                    |
| **Single gateway replica**          | In-process registry, no cross-pod drain.                                                                                                                                                       | Multi-replica with pod-IP routing + drain protocol (Phase 2).                                                                 |
| **`sample-mcp` echo server**        | Stand-in MCP for round-trip proof.                                                                                                                                                             | The customer's real MCP server.                                                                                               |
| **ngrok host**                      | Public tunnel so the agent on a separate machine can dial the kind gateway's ingress.                                                                                                          | Normal external ingress hostname + DNS.                                                                                       |

---

## Tests

```bash
go test ./tunnel/... ./server/internal/tunnels/...
```

- `tunnel/e2e` — real yamux-over-WS: connect → forward → echo; tenant-isolation
  502; revoke kills the session.
- `tunnel/wire` — WSConn + yamux stability and the http.Serve/http.Client
  topology.
- `server/internal/tunnels` — serve path forwards with the injected tunnel-ID
  header; no-route 502.

---

## Scope — deferred

Per the design, intentionally not in the POC:

- Postgres `tunnels` table, Goa `/rpc/tunnels.*` API, audit/RBAC wiring.
- Hooking `internal/tunnels.ServeTunnel` into `internal/mcp`'s `serveendpoint` +
  the remotemcp interceptor chain (authz, usage limits, usage tracking,
  ClickHouse logging) — a one-call-site change so tunnel traffic is metered
  exactly like remotemcp traffic.
- Multi-replica routing and the drain protocol.
- Real per-tunnel key mint/rotate (the management API).
