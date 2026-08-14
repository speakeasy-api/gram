# Reaching a dev stack from another machine

The Go services already bind every interface, but everything the stack hands a
browser is an absolute URL built from `localhost`. So another machine can open a
TCP connection to the dev server and still be told to go look at itself — the app
shell loads, then login redirects to a host that machine cannot reach.

`mise run zero:remap-hostname` fixes that by pointing the browser-facing URLs at
a hostname other machines can reach, while leaving everything this box dials
itself on `localhost`. It also opens up the admin dashboard's dev server, which
binds loopback only until you opt in.

This is opt-in and inert until you run it. Nothing in `./zero` sets it up, and
the stack behaves exactly as before on a machine that never opts in.

> [!WARNING]
> **Read this before pointing a shared network at a dev stack.** The local stack
> has no meaningful authentication: `mock-oidc` and `dev-idp` mint a session for
> any identity you type, with no password, so anyone who can reach the admin
> dashboard can sign in as an admin. Independently of this feature, `compose.yml`
> already publishes Postgres, Redis, Temporal, Grafana and ClickHouse on
> `0.0.0.0` with default credentials, and the Go services already listen on every
> interface.
>
> In other words the dev stack is only ever protected by the network it sits on.
> Opting in makes it _convenient_ to reach across a network, so use one you trust
> — a private tailnet is fine, a café or shared office LAN is not. On an untrusted
> network, prefer an SSH tunnel that forwards only the ports you need.

## When you'd want it

You run the stack on one machine and want to reach it from your own laptop or
local device — a dev box under the desk, a cloud VM, a spare machine. Anything
giving both machines a route works: Tailscale, a VPN, a LAN, an SSH tunnel, as
long as the browser can connect to the dev server's ports. Tailscale is the
worked example below because `--detect` can read the hostname straight out of
it.

You do **not** need this to browse the stack on the machine running it.

## Setup

Two machines are involved, and it matters which one you are on. Below, **host**
is the machine running the stack and **client** is the machine you browse from.

### 1. On the host

```sh
# Read the hostname from the local tailscale daemon
mise run zero:remap-hostname --detect

# ...or name it yourself
mise run zero:remap-hostname --host devbox.example.ts.net

./zero
```

`./zero` regenerates the TLS certificate — the new hostname joins `localhost` and
`host.docker.internal` on the same cert — and restarts the stack.

Settings land in `mise.local.toml`, which is gitignored, so your hostname never
leaves the machine.

### 2. On the client: trust the certificate

**This step cannot be done from the host, and skipping it means every page shows
a certificate warning.**

The stack uses a [mkcert](https://github.com/FiloSottile/mkcert) certificate
signed by a root CA that exists only on the host. The browser is on the client,
so the client needs that CA. Copy `$(mkcert -CAROOT)/rootCA.pem` over — the task
prints its full path — and trust it once:

```sh
# on the CLIENT
sudo security add-trusted-cert -d -r trustRoot \
  -k /Library/Keychains/System.keychain rootCA.pem
```

Understand what you are granting: whoever can read `rootCA-key.pem` on the host
can mint a certificate your client will trust for _any_ domain, not just this
stack. That is fine for a machine you control; think twice on a shared box.

### 3. On the client: check it

The port is the host's `GRAM_SITE_PORT` — randomized per worktree, and printed by
the task. Substitute it below; it is not set in the client's shell.

```sh
# on the CLIENT — -k first, so a routing problem is not confused with a cert one
curl -k https://<your-host>:<site-port>/

# then without -k, which also confirms the CA is trusted
curl https://<your-host>:<site-port>/
```

Then browse `https://<your-host>:<site-port>`.

### If you lack admin rights on the host

`mkcert -install` will fail there and abort `./zero`. Set
`GRAM_TLS_SKIP_CA_TRUST=1` in `mise.local.toml` to downgrade that to a warning.
Registering the CA on the host only matters for a browser running on the host;
everything headless is pointed at the CA file explicitly, and the client needs
the CA in its own trust store either way.

## Worktrees

New worktrees inherit the setting. `git:workinit` copies `mise.local.toml` from
the main worktree and re-applies the hostname after remapping ports, so a fresh
worktree comes up remotely reachable on its own randomized ports with no extra
steps. `wt list`'s URL column shows the reachable hostname too.

## Reverting

```sh
mise run zero:remap-hostname --reset
./zero
```

## The part worth understanding

The task is **not** a find-and-replace of `localhost`. It moves an explicit
allowlist of URLs — the ones a browser opens — and nothing else. The list lives
in `browserFacing()` in `.mise-tasks/zero/remap-hostname.mts`, with the reasoning
per entry.

The allowlist direction is deliberate. Anything not named keeps its local value,
so an env var added later stays on `localhost` until somebody moves it on
purpose: the failure mode is "a link still says localhost", not "login breaks and
nobody knows why". An earlier version worked the other way round and silently
mis-classified a dev-proxy target as browser-facing.

Two examples of why the distinction matters:

- **The admin dashboard's OIDC emulator.** The admin server fetches OIDC
  discovery from `GRAM_ADMIN_OIDC_EMULATOR_URL`, and go-oidc requires the
  document's `issuer` to match the URL it fetched from. Moving the issuer breaks
  discovery outright. So the emulator keeps a local issuer and advertises a
  remote `authorization_endpoint` instead, via `MOCK_OIDC_BROWSER_BASE_URL` — the
  only URL in that document a browser ever visits.
- **`GRAM_SERVER_URL` is genuinely both.** The dashboard bakes it into
  operator-facing URLs, but `mise run seed`, the Gram CLI, `smoke:platform-mcp`
  and the local functions runner all dial it from the host. It therefore stays
  local, and the browser-facing half gets its own `GRAM_SERVER_PUBLIC_URL`.

Whether a URL on the wrong side fails loudly or silently depends on the tunnel.
Under Tailscale's userspace networking mode — a `tailscaled` you can run without
root — the host generally has no route to its own tailnet address even though the
name resolves, so it fails outright. With a kernel-mode tailnet, or a leftover
`utun` route from a previous Tailscale install, the same misconfiguration can
appear to work on the machine you developed it on and break on someone else's.

If you add an env var holding an absolute URL, decide who dials it. Add it to
`browserFacing()` only if the answer is "a browser".

## Troubleshooting

| Symptom                                         | Cause                                                                                                                                                              |
| ----------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Connection refused from the client              | No route to the port. Check the tunnel/VPN is up on both ends and nothing is firewalling the port.                                                                 |
| Connection refused for the admin dashboard only | Its dev server binds loopback until you opt in. Re-run the task, then restart it — a stale `admin-dashboard` daemon started before opting in is still on loopback. |
| Certificate warnings                            | The root CA is not trusted on the client — see step 2.                                                                                                             |
| Login redirects to `localhost`                  | The stack was booted before the task ran. Re-run the task, then `./zero`.                                                                                          |
| Admin writes 403 from the host's own browser    | `GRAM_ADMIN_ALLOWED_ORIGINS` must name the origin the browser is actually on. The task lists both; a hand-edited value may not.                                    |
| Dashboard loads, API calls fail                 | Something moved `GRAM_SERVER_BACKEND_URL` off `localhost`; it is the dev proxy's target and must stay local.                                                       |
| `mise run seed` / `smoke:platform-mcp` fail     | Something moved `GRAM_SERVER_URL` off `localhost`. Only `GRAM_SERVER_PUBLIC_URL` should carry the remote hostname.                                                 |
| Live reload stops working                       | `VITE_DEV_HOSTNAMES` should list the hostname you browse on; re-run the task. It affects HMR only, not whether the app loads.                                      |
