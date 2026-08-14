#!/usr/bin/env -S node

//MISE dir="{{ config_root }}"
//MISE description="Point this worktree's browser-facing URLs at a hostname other machines can reach (e.g. a Tailscale node), so a laptop can open the dev stack running on this box."

//USAGE flag "--host <host>" help="Hostname other machines reach this box on, e.g. devbox.example.ts.net. Mutually exclusive with --detect."
//USAGE flag "--detect" help="Read the hostname from the local tailscale daemon instead of passing --host."
//USAGE flag "--reset" help="Drop the overrides and go back to localhost."

/**
 * Everything the dev stack hands a browser is an absolute URL built from
 * `localhost` (see mise.toml), which is unreachable from anywhere else — so a
 * second machine can open a TCP connection to the dev server and still get a
 * login redirect pointing at itself.
 *
 * This task overrides those URLs in `mise.local.toml`. It is deliberately NOT a
 * blanket rewrite: plenty of URLs in mise.toml are dialed by processes ON this
 * box, and moving those breaks the stack in ways that only surface later (a
 * failing seed, a dev proxy that cannot reach its target, an OIDC discovery
 * fetch that 404s).
 *
 * ## The allowlist is the whole design
 *
 * `browserFacing()` below is an explicit list, and it is the ONLY thing that
 * moves. Anything not named there keeps whatever mise.toml and the worktree's
 * port remap give it. That default is the point: an env var added to mise.toml
 * next year stays on localhost until somebody deliberately moves it, so the
 * failure mode is "a link still says localhost" rather than "login breaks and
 * nobody knows why".
 *
 * An earlier version worked the other way round — rewrite `GRAM_HOST` and
 * everything transitively derived from it, then pull individual vars back with a
 * denylist. That is fail-open, and it silently mis-classified
 * `GRAM_ADMIN_BACKEND_URL` (a dev-proxy target, dialed here) as browser-facing.
 * Do not reintroduce it.
 *
 * ## Notes on specific entries
 *
 *   GRAM_SERVER_PUBLIC_URL   `GRAM_SERVER_URL` is genuinely dual-use: the
 *                            dashboard bakes it in for operator-facing URLs, but
 *                            `mise run seed`, the Gram CLI, `smoke:platform-mcp`
 *                            and the local functions runner all dial it from
 *                            this box. So GRAM_SERVER_URL stays local and the
 *                            browser-facing half gets its own var, which
 *                            client/dashboard/vite.config.ts prefers when set.
 *
 *   GRAM_APP_URL             Baked into the admin dashboard, which builds an
 *                            impersonation link out of it
 *                            (client/admin/src/lib/impersonation.ts) for an
 *                            operator to click. It defaults to GRAM_SERVER_URL,
 *                            which stays local, so without this the link would
 *                            be dead on the machine doing the browsing.
 *
 *   GRAM_ADMIN_ALLOWED_ORIGINS
 *                            Lists BOTH origins. AdminOriginCheck 403s every
 *                            unsafe method whose Origin is not named, so listing
 *                            only the remote origin would stop admin writes
 *                            working in a browser on this box.
 *
 *   GRAM_IDP_BASE_URL        The browser navigates to `${it}/authorize`
 *                            (server/internal/auth/identity/identity.go). The
 *                            code exchange goes to WORKOS_API_URL, which is not
 *                            listed here and stays local.
 *
 *   MOCK_OIDC_BROWSER_BASE_URL
 *                            The admin dashboard's fake Google IdP. Only its
 *                            discovery `authorization_endpoint` moves:
 *                            GRAM_ADMIN_OIDC_EMULATOR_URL is fetched by the
 *                            admin server and go-oidc requires the document's
 *                            issuer to match the URL it fetched from, so the
 *                            issuer cannot move (mock-oidc/handlers.go).
 *
 *   VITE_DEV_HOSTNAMES       Only backs vite's HMR websocket. Vite skips its
 *                            host-validation middleware entirely when the dev
 *                            server is HTTPS, which this stack always is, so
 *                            this does not gate who may load the app.
 *
 * TLS needs no special handling: zero:tls derives its mkcert SANs from
 * GRAM_SITE_URL and regenerates on every `./zero`, so the remote hostname joins
 * localhost and host.docker.internal on one cert, covering every port.
 *
 * `GRAM_DEV_HOSTNAME` is written first as a marker recording the intent. Ports
 * are randomized per worktree, so `git:workinit` re-emits port-dependent vars
 * into mise.local.toml AFTER copying it from the main worktree, which would
 * clobber these overrides; workinit re-runs this task when the marker is present
 * so they always land last.
 */

import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { parseTOML } from "confbox";

const LOCAL_FILE = "mise.local.toml";

/** Records the operator's intent so `git:workinit` can re-apply after remapping ports. */
const MARKER = "GRAM_DEV_HOSTNAME";

/**
 * The allowlist: every URL a BROWSER opens, and nothing else. Values are mise
 * templates so they track whatever ports this worktree was assigned. See the
 * header comment before adding an entry — the question to answer is "who dials
 * this, a browser or a process on this box?", and anything dialed here must not
 * be listed.
 */
function browserFacing(host: string): Record<string, string> {
  return {
    GRAM_SITE_URL: `https://${host}:{{env.GRAM_SITE_PORT}}`,
    GRAM_SERVER_PUBLIC_URL: `https://${host}:{{env.GRAM_SERVER_PORT}}`,
    GRAM_APP_URL: `https://${host}:{{env.GRAM_SERVER_PORT}}`,
    GRAM_ADMIN_SERVER_URL: `https://${host}:{{env.GRAM_ADMIN_DASHBOARD_PORT}}`,
    GRAM_ADMIN_ALLOWED_ORIGINS: `https://${host}:{{env.GRAM_ADMIN_DASHBOARD_PORT}},https://localhost:{{env.GRAM_ADMIN_DASHBOARD_PORT}}`,
    GRAM_IDP_BASE_URL: `http://${host}:{{env.GRAM_DEVIDP_PORT}}/oauth2`,
    MOCK_OIDC_BROWSER_BASE_URL: `http://${host}:{{env.GRAM_ADMIN_OIDC_EMULATOR_PORT}}`,
    VITE_DEV_HOSTNAMES: `localhost,devbox,${host}`,
  };
}

/** Keys this task owns, for --reset and for clearing stale state before a re-run. */
const OWNED = [MARKER, ...Object.keys(browserFacing("placeholder"))];

/**
 * Keys an earlier revision of this task wrote and this one must not leave behind.
 * It rewrote `GRAM_HOST` / `GRAM_ADMIN_HOST` and let mise's own templating carry
 * the change into everything derived from them; the allowlist above replaces
 * that. A leftover `GRAM_HOST` override would silently drag GRAM_SERVER_URL and
 * GRAM_API_URL remote behind the allowlist's back, breaking `mise run seed` and
 * the CLI with no obvious cause. Only reachable on a checkout that ran the older
 * revision, so this can go once that is safely in the past.
 */
const LEGACY_KEYS = [
  "GRAM_HOST",
  "GRAM_ADMIN_HOST",
  "GRAM_SERVER_URL",
  "GRAM_API_URL",
  "GRAM_ADMIN_BACKEND_URL",
];

function mise(...args: string[]): string {
  // stderr is dropped: `mise unset` complains when mise.local.toml does not
  // exist yet, and a wall of red before the success banner reads like failure.
  return execFileSync("mise", args, {
    encoding: "utf-8",
    stdio: ["ignore", "pipe", "ignore"],
  });
}

function unset(key: string): void {
  try {
    mise("unset", "--file", LOCAL_FILE, key);
  } catch {
    // Not present — nothing to clear.
  }
}

/**
 * mise resolves `{{env.X}}` against declarations already seen, so a var must be
 * written after the ones it references. `mise set` appends, and unsetting first
 * moves a key that already exists to the end — the same ordering dance
 * `git:workinit` does for ports.
 */
function set(key: string, value: string): void {
  unset(key);
  mise("set", "--file", LOCAL_FILE, `${key}=${value}`);
}

/**
 * Reads the tailnet MagicDNS name from whichever tailscaled this user can talk
 * to, most specific first: an explicitly configured socket, then a per-user
 * daemon, then the system one.
 *
 * Order matters. A machine can have a system tailscaled installed but dormant —
 * or still holding a stale identity from an earlier registration — alongside the
 * userspace daemon the developer actually runs. Probing the system socket first
 * picks up that stale name and points the whole stack at a host nobody can
 * reach. `undefined` means "let the CLI use its default socket", so it belongs
 * last and must not be conflated with an unset TAILSCALE_SOCKET.
 */
function detectHost(): string {
  const sockets: (string | undefined)[] = [
    ...(process.env["TAILSCALE_SOCKET"]
      ? [process.env["TAILSCALE_SOCKET"]]
      : []),
    `${process.env["HOME"]}/.tailscale/tailscaled.sock`,
    undefined, // system daemon at its default path
  ];

  for (const socket of sockets) {
    const args = socket
      ? ["--socket", socket, "status", "--json"]
      : ["status", "--json"];
    try {
      const raw = execFileSync("tailscale", args, {
        encoding: "utf-8",
        stdio: ["ignore", "pipe", "ignore"],
      });
      // Parsing stays inside the try: a daemon that exits 0 with something
      // other than JSON must fall through to the next candidate, not abort.
      const name = JSON.parse(raw)?.Self?.DNSName;
      // MagicDNS names come back fully qualified with a trailing dot.
      if (typeof name === "string" && name.length > 1) {
        return name.replace(/\.$/, "");
      }
    } catch {
      continue;
    }
  }

  throw new Error(
    "Could not read a hostname from tailscale. Is the daemon logged in? Pass --host explicitly instead.",
  );
}

/**
 * The host is interpolated into URLs that already carry their own scheme and
 * port, so a value that brings either along produces something like
 * `https://https://devbox:5173:5173` — written to every browser-facing var at
 * once, and only visible later as a stack that will not load. Cheaper to refuse.
 */
function validateHost(host: string): string {
  // A bracketed IPv6 literal is the one legal way for a bare host to carry
  // colons, and it is already in the form a URL wants.
  const bare = /^\[.+\]$/.test(host)
    ? host
    : (host
        .replace(/^[a-z][a-z0-9+.-]*:\/\//i, "")
        .split("/")[0]
        ?.split(":")[0] ?? "");

  if (bare !== host) {
    throw new Error(
      `--host takes a bare hostname, not a URL: got ${host}. Ports and scheme come from this worktree's own mapping — try --host ${bare}`,
    );
  }
  return host;
}

function miseTomlEnv(): Record<string, string> {
  const config = parseTOML(readFileSync("mise.toml", "utf-8")) as {
    env: Record<string, string>;
  };
  return config.env;
}

function currentMarker(): string | undefined {
  try {
    const local = parseTOML(readFileSync(LOCAL_FILE, "utf-8")) as {
      env?: Record<string, string>;
    };
    return local.env?.[MARKER];
  } catch {
    return undefined;
  }
}

/**
 * Records the hostname for `wt list`'s URL column. Best-effort display metadata:
 * a worktree without `wt`, or outside one, must not fail the task.
 */
function setWorktreeVar(host: string): void {
  try {
    execFileSync("wt", ["config", "state", "vars", "set", `devhost=${host}`], {
      stdio: "ignore",
    });
  } catch {
    // No wt, or not inside a worktree — the URL column falls back to localhost.
  }
}

/**
 * Undoing an override is not the same as deleting the key. `zero:remap-ports`
 * also emits several of these (they depend on a `_PORT`), and it emits them into
 * mise.local.toml precisely because mise.toml's copy would resolve against
 * mise.toml's DEFAULT ports, not this worktree's. Unsetting therefore does not
 * restore the worktree's value — it silently points the key at the primary
 * worktree's ports, so a worktree's admin proxy and dev-idp would address
 * another worktree's stack, with its own database, and nothing would look wrong.
 *
 * Re-emitting mise.toml's own template instead resolves against the ports
 * already declared above it in this file, which is the value the worktree
 * should have had all along.
 */
function restore(key: string, miseTomlValue: string | undefined): void {
  if (typeof miseTomlValue === "string") {
    set(key, miseTomlValue);
  } else {
    unset(key);
  }
}

function reset(): void {
  const env = miseTomlEnv();
  for (const key of [...LEGACY_KEYS, ...OWNED]) {
    restore(key, env[key]);
  }
  setWorktreeVar("localhost");
  console.log(
    "✅ Reverted to localhost. Re-run `./zero` to regenerate certs and restart.",
  );
}

function main(): void {
  if (process.env["usage_reset"] === "true") {
    reset();
    return;
  }

  const flagHost = process.env["usage_host"]?.trim();
  const detect = process.env["usage_detect"] === "true";
  if (flagHost && detect) {
    throw new Error("Pass either --host or --detect, not both.");
  }
  if (flagHost) validateHost(flagHost);

  // With no flags, re-apply whatever is already recorded. This is the path
  // `git:workinit` takes on a fresh worktree.
  const host = flagHost || (detect ? detectHost() : currentMarker());
  if (!host) {
    throw new Error(
      "No hostname configured. Run with --detect (reads it from tailscale) or --host <name>.",
    );
  }

  const env = miseTomlEnv();

  // Clear first so a re-run with a different host cannot leave a stale override
  // sitting after the vars that should have replaced it. The legacy keys are
  // restored rather than unset, for the reason `restore` explains: several are
  // port-dependent, and dropping them points this worktree at another one's.
  for (const key of LEGACY_KEYS) restore(key, env[key]);
  for (const key of OWNED) unset(key);

  set(MARKER, host);
  for (const [key, value] of Object.entries(browserFacing(host))) {
    set(key, value);
  }
  setWorktreeVar(host);

  const sitePort = process.env["GRAM_SITE_PORT"] ?? "5173";

  console.log(`✅ Browser-facing URLs now point at ${host}`);
  console.log();
  console.log("Next steps:");
  console.log();
  console.log("  1. On THIS machine:  ./zero");
  console.log(
    "     Regenerates the TLS certificate for the new hostname and restarts the stack.",
  );
  console.log();
  console.log(
    "  2. On the machine you BROWSE FROM — nobody can do this step for you, and",
  );
  console.log(
    "     without it every page shows a certificate warning. Copy this file over",
  );
  console.log("     and trust it, once:");
  console.log();
  console.log(`       ${caRootFile()}`);
  console.log();
  console.log(
    "       macOS: sudo security add-trusted-cert -d -r trustRoot \\",
  );
  console.log(
    "                -k /Library/Keychains/System.keychain rootCA.pem",
  );
  console.log(
    "       else:  install mkcert on that machine and run `mkcert -install`",
  );
  console.log();
  console.log("  3. From that same machine, check it:");
  console.log(`       curl -k https://${host}:${sitePort}/`);
  console.log();
  console.log(
    "Opting in exposes this stack to everything that can route to this box —",
  );
  console.log(
    "see the security note in docs/remote-dev-access.md before pointing a",
  );
  console.log("shared network at it.");
}

/** Path to the mkcert root CA, for the copy-it-over instruction. */
function caRootFile(): string {
  try {
    const root = execFileSync("mkcert", ["-CAROOT"], {
      encoding: "utf-8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
    if (root) return `${root}/rootCA.pem`;
  } catch {
    // mkcert not resolvable — fall back to naming the file generically.
  }
  return '"$(mkcert -CAROOT)"/rootCA.pem';
}

main();
