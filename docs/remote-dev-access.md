# Reaching a dev stack from another machine

The local stack binds every interface already, but everything it hands a browser
is an absolute URL built from `localhost`. So another machine can open a TCP
connection to the dev server and still be told to go look at itself — the app
shell loads, then login redirects to a host that machine cannot reach.

`mise run zero:remap-hostname` fixes that by pointing the browser-facing URLs at
a hostname other machines can reach, while leaving the URLs this box dials
itself on `localhost`.

This is opt-in and inert until you run it. Nothing in `./zero` sets it up, and
the stack behaves exactly as before on a machine that never opts in.

## When you'd want it

You run the stack on one machine and browse it from another: a dev box under the
desk, a cloud VM, a spare laptop. Anything giving both machines a route works —
Tailscale, a VPN, a LAN — as long as the browser can reach the dev server's
ports. Tailscale is the worked example below because `--detect` can read the
hostname straight out of it.

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

### 3. On the client: check it

```sh
# on the CLIENT — -k first, so a routing problem is not confused with a cert one
curl -k https://<your-host>:$GRAM_SITE_PORT/

# then without -k, which also confirms the CA is trusted
curl https://<your-host>:$GRAM_SITE_PORT/
```

Then browse `https://<your-host>:$GRAM_SITE_PORT`.

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

The task is deliberately **not** a find-and-replace of `localhost`. Some URLs are
dialed by processes on the box running the stack, and those must stay local — the
task's header comment carries the current list and the reasoning. Two examples of
why the distinction is load bearing:

- **The admin dashboard's OIDC emulator.** The admin server fetches OIDC
  discovery from `GRAM_ADMIN_OIDC_EMULATOR_URL`, and go-oidc requires the
  document's `issuer` to match the URL it fetched from. Moving the issuer breaks
  discovery outright. So the emulator keeps a local issuer and advertises a
  remote `authorization_endpoint` instead, via `MOCK_OIDC_BROWSER_BASE_URL` — the
  only URL in that document a browser ever visits.
- **The vite dev proxies.** `GRAM_SERVER_BACKEND_URL` and
  `GRAM_ADMIN_BACKEND_URL` are dialed by the dev servers themselves, not by
  browsers, so they stay on `localhost` even though the second is derived from
  `GRAM_ADMIN_HOST`.

This matters most under Tailscale's userspace networking mode (a `tailscaled` you
can run without root), where the box cannot route to its own tailnet address at
all even though the name resolves. There, a URL on the wrong side of the split
fails outright rather than merely taking a slow path.

If you add an env var holding an absolute URL, put it on the correct side of that
split in `zero:remap-hostname`.

## Troubleshooting

| Symptom                                   | Cause                                                                                                                     |
| ----------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| Connection refused from the other machine | No route to the port. Check the tunnel/VPN is up on both ends and nothing is firewalling the port.                        |
| `403` / "host not allowed"                | Vite rejects unknown `Host` headers. `VITE_DEV_HOSTNAMES` should list your hostname; re-run the task.                     |
| Certificate warnings                      | The root CA is not trusted on the browsing machine — see above.                                                           |
| Login redirects to `localhost`            | The stack was booted before the task ran, or a var landed on the wrong side of the split. Re-run the task, then `./zero`. |
| Dashboard loads, API calls fail           | `GRAM_SERVER_BACKEND_URL` was moved off `localhost`; it is the dev proxy's target and must stay local.                    |
