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
MCP server that speaks the stdio transport. Stdio mode needs a Unix agent,
such as the published Linux image, because shutdown relies on process groups.
The agent runs the command with `/bin/sh -c` and bridges Streamable HTTP onto
it:

- Each MCP session gets its own server process, started by `initialize` and
  addressed by an agent-minted `Mcp-Session-Id`.
- Messages are relayed verbatim. Server-initiated requests and notifications go
  to the session's GET stream when one is open, otherwise to an open POST
  stream, otherwise to a bounded backlog drained by the next GET stream.
- DELETE, `TUNNEL_STDIO_IDLE_TIMEOUT` (default `30m`), and agent shutdown stop
  the process: stdin closes, then SIGTERM, then SIGKILL to its process group.
  The idle timer counts MCP messages in either direction; an open GET stream
  with nothing on it does not keep a session alive.
- `TUNNEL_STDIO_MAX_SESSIONS` (default `16`) caps concurrent processes; an
  `initialize` past the cap gets HTTP 503.
- A single message over 32 MiB in either direction is refused; from the server
  it ends the session. A stream whose client falls 64 MiB behind is cut off
  rather than stalling the session.
- A server that stops reading stdin while a request is being written to it is
  stopped, so a stuck server cannot hold a session slot.
- The agent answers a server's `ping` sent before `initialize` completes, since
  the client has no way to answer it yet.
- The process inherits the agent's environment minus every `TUNNEL_*`
  variable, so credentials for the server go in the agent's environment.
- Processes outlive gateway reconnects, but sessions are local to one agent.
  Run a single agent replica per stdio tunnel.

Only MCP traffic at the root path is served. OAuth back-channel paths return
404, so a stdio tunnel cannot back an OAuth issuer.

The published image contains no language runtimes. To run a Node or Python
server, build on top of it and add the runtime the command needs.

## Per-User Credentials for Stdio Servers

A stdio server can act upstream as each Speakeasy user. Set
`TUNNEL_STDIO_CREDENTIALS=user` and the agent gives every MCP session its own
server process and a file holding that user's upstream access token. The
server reads the file named by `SPEAKEASY_ACCESS_TOKEN_FILE` each time it calls
the upstream API. It never runs its own sign-in.

Speakeasy owns sign-in and refresh. A user connects the upstream account once
in Speakeasy (authorization code with PKCE); Speakeasy stores and refreshes the
tokens in its encrypted storage. On each request to a private tunneled MCP
server with a linked upstream grant, it forwards the user's current access
token as `Authorization: Bearer` together with a signed `X-Speakeasy-Identity`
assertion. The assertion names the user, the MCP server and the exact grant,
and carries the SHA-256 of the token it vouches for (see the
[signed caller identity guide](../docs/tunnel-identity.md)).

### What the agent enforces

- Every request must carry a valid assertion for this tunnel: RS256 signed by
  a key in the configured JWKS, the configured issuer, audience and
  organization, at most 60 seconds long. Missing or invalid assertions get
  HTTP 401 and touch no session.
- Each session belongs to one principal: issuer, subject, organization, MCP
  server and whether the session is consent discovery. Another principal's
  request for that session gets 404 exactly like an unknown session and does
  not affect it.
- Every POST must carry a bearer whose SHA-256 matches the assertion's
  `upstream_credential.token_sha256`, from a grant the subject owns
  (`owner: "subject"`). An `initialize` without one gets 401 and starts no
  process. For an existing session the principal is proven, so a POST without
  a valid credential (for example after the user unlinks the account) gets 401
  and ends the session.
- A session is bound to its grant: client, grant row and grant generation. A
  refreshed token for the same grant replaces the file in the running session.
  Reauthorizing, switching upstream account or another client ends the session
  (404) so the client starts over with a fresh process.
- Consent discovery sessions only admit the methods their assertion allows.
- GET and DELETE need only a valid assertion from the session's principal, so
  a user can always end their own session.

Tokens within one grant are interchangeable, so the file holds the token of
the last admitted POST, and makes no promise about order: a request that was
delayed can put back an older token of the same grant, and the server may read
a token written for a later request. The server should reopen the file for
each upstream call and report an upstream rejection as an error rather than
fall back to other credentials; nothing guarantees that the next request
brings a newer token. A token whose stated expiry has passed is never written.

A session ends at the earlier of its token's stated expiry plus 30 seconds and
`TUNNEL_STDIO_CREDENTIALS_MAX_AGE` (default `1h`) after the last admitted
token, unless a newer token arrives first. When the user unlinks or
reauthorizes the account, the session ends at the next request that reaches
the agent, or at that deadline if Speakeasy stops forwarding requests. Stopping
the session can take up to about 40 seconds more (an in-flight stdin write,
then stdin close, SIGTERM and SIGKILL to the process group).

### Configuration

| Variable                           | Meaning                                                                                                              |
| ---------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `TUNNEL_STDIO_CREDENTIALS`         | `user` enables per-user credentials. Unset keeps the default behavior.                                               |
| `TUNNEL_IDENTITY_ISSUER`           | Exact assertion issuer, an origin without a trailing slash: `https://tunnel.speakeasy.com`.                          |
| `TUNNEL_IDENTITY_AUDIENCE`         | The tunneled source's saved resource identifier, or `tunneled-mcp-server:<TUNNELED_MCP_SERVER_ID>` when it has none. |
| `TUNNEL_IDENTITY_ORGANIZATION_ID`  | Your Speakeasy organization ID, shown under **Caller Identity** in the MCP server's settings.                        |
| `TUNNEL_IDENTITY_JWKS_URL`         | Verification keys. Defaults to `<issuer>/.well-known/jwks.json`. Never taken from a token.                           |
| `TUNNEL_IDENTITY_ALLOW_INSECURE`   | `true` admits `http://` issuer and JWKS URLs on localhost or `host.docker.internal`, for local development only.     |
| `TUNNEL_STDIO_CREDENTIALS_DIR`     | Memory-backed directory for token files. Defaults to `/dev/shm`.                                                     |
| `TUNNEL_STDIO_CREDENTIALS_MAX_AGE` | Longest a session keeps a token after its last write, whatever its stated expiry. Defaults to `1h`.                  |

The agent refuses to start in this mode unless it runs on Linux with a stdio
command, the verifier settings are valid, the credentials directory is on
tmpfs or ramfs, and `SPEAKEASY_ACCESS_TOKEN_FILE` is not set in its own
environment. It fetches verification keys on demand, trusts them for five
minutes, and rejects every assertion when it cannot revalidate expired keys.

### Server process contract

The agent sets `SPEAKEASY_ACCESS_TOKEN_FILE`, `HOME`, `XDG_CONFIG_HOME` and
`XDG_DATA_HOME` for each process to paths inside a private session directory,
replacing inherited values, and removes `OKTA_ACCESS_TOKEN_FILE`. The server
must read the token file when it calls the upstream API rather than caching it
at startup; the file is written before the process starts and replaced
atomically. Package caches such as `XDG_CACHE_HOME` stay shared across
sessions; keep them free of credentials.

The server's stderr is not logged in this mode, because SDK errors can print
credentials. The agent logs only its size.

### Storage and host requirements

Token files are written only to a memory-backed filesystem, under an
agent-owned `speakeasy-tunnel-agent/<instance>/` directory (mode `0700`,
files `0600`), and removed when the session ends: once its processes have
stopped, or after a bounded attempt to stop them fails. At startup the agent removes
files left by agents that crashed, and never touches anything else in the
directory. Several agents may share a credentials directory.

This promises that no credential file is written to a persistent filesystem.
It does not stop the kernel from writing memory to disk. For that, also:

- disable swap on the host, or mount the directory as tmpfs with `noswap`;
- disable core dumps for the container;
- avoid host hibernation.

Run the agent as the container's main process, under a minimal init such as
`docker run --init` or tini, and start the server with `exec`, so stopping the
container stops every server process. The agent also asks the kernel to kill
its direct child if the agent dies. A server that daemonizes or starts a new
session escapes its process group; the agent removes its token file anyway
when the session ends.

On Kubernetes, mount `emptyDir: {medium: Memory}` at the credentials
directory, and set `securityContext` with `runAsNonRoot`,
`allowPrivilegeEscalation: false` and `readOnlyRootFilesystem` where your
server allows it.

Every server process runs as the agent's user. File permissions do not
isolate processes of the same user from each other, so run only a trusted,
pinned server binary in this mode.

### Callers that cannot use this mode

The whole tunnel switches to this mode, so every MCP server on the tunnel must
be private and its callers must have a linked per-user grant. These fail
closed with 401: public MCP servers, anonymous callers, background connection
probes, API keys and agents without their own subject grant, the MCP server's
own client credentials (`owner: "self"`), and tokens obtained by identity
chaining. Assertions from Speakeasy versions that do not send
`mcp_server_id` and `upstream_credential` are refused.

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
