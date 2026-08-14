#!/usr/bin/env -S node

//MISE dir="{{ config_root }}"
//MISE description="Point this worktree's browser-facing URLs at a hostname other machines can reach (e.g. a Tailscale node), so a laptop can open the dev stack running on this box."

//USAGE flag "--host <host>" help="Hostname other machines reach this box on, e.g. devbox.example.ts.net. Mutually exclusive with --detect."
//USAGE flag "--detect" help="Read the hostname from the local tailscale daemon instead of passing --host."
//USAGE flag "--reset" help="Drop the overrides and go back to localhost."

/**
 * Everything the dev stack hands a browser is an absolute URL built from
 * `GRAM_HOST` / `GRAM_ADMIN_HOST` (see mise.toml). On a devbox those default to
 * `localhost`, which is unreachable from anywhere else — so a laptop on the same
 * tailnet can open a TCP connection to the dev server and still get a login
 * redirect pointing at its own machine.
 *
 * This task rewrites those hostnames in `mise.local.toml` and re-emits every env
 * var that derives from them. It is deliberately NOT a blanket find-and-replace
 * of `localhost`: some URLs are dialed by processes ON this box and must stay
 * local. The split that matters:
 *
 *   opened by a browser (must be the remote hostname)
 *     GRAM_SITE_URL, GRAM_SERVER_URL, GRAM_API_URL — derived from GRAM_HOST
 *     GRAM_ADMIN_SERVER_URL, GRAM_ADMIN_ALLOWED_ORIGINS
 *                                 — derived from GRAM_ADMIN_HOST. The allowed
 *                                   origins have to name the browser's origin
 *                                   exactly or every admin write 403s.
 *     GRAM_IDP_BASE_URL           — the browser navigates to
 *                                   `${GRAM_IDP_BASE_URL}/authorize`
 *                                   (server/internal/auth/identity/identity.go)
 *     MOCK_OIDC_BROWSER_BASE_URL  — same idea for the admin dashboard's fake
 *                                   Google IdP; only its authorization_endpoint
 *                                   moves (mock-oidc/handlers.go)
 *
 *   dialed by a process on this box (must stay localhost)
 *     WORKOS_API_URL          — the server's REST endpoint for dev-idp's
 *                               mock-workos emulator (server/cmd/gram/deps.go)
 *     GRAM_SERVER_BACKEND_URL — the dashboard's vite dev proxy target
 *                               (client/dashboard/vite.config.ts)
 *     GRAM_ADMIN_BACKEND_URL  — the admin dashboard's vite dev proxy target.
 *                               Derived from GRAM_ADMIN_HOST, so the dependent
 *                               walk below rewrites it and PINNED_LOCAL puts it
 *                               back (client/admin/vite.config.ts).
 *     GRAM_DEVIDP_EXTERNAL_URL — WORKOS_API_URL is derived from it, so leaving
 *                               GRAM_DEVIDP_HOST alone keeps both local while
 *                               GRAM_IDP_BASE_URL moves on its own.
 *     GRAM_ADMIN_OIDC_EMULATOR_URL — the admin server fetches OIDC discovery
 *                               from here, and go-oidc requires the document's
 *                               issuer to match the URL it fetched from. So the
 *                               emulator keeps a localhost issuer and advertises
 *                               a remote authorization_endpoint instead, via
 *                               MOCK_OIDC_BROWSER_BASE_URL.
 *
 * The OAuth callbacks need nothing: mock-oidc's client declares its redirect_uris
 * as ${GRAM_ADMIN_SERVER_URL}/admin/auth.callback and expands them at load
 * (mock-oidc/config.go), so they follow the browser-facing value already.
 *
 * The two-list split is load bearing, not merely tidy: under Tailscale's
 * userspace networking mode this box cannot route to its own tailnet address at
 * all, even though the name resolves. A blanket find-and-replace of `localhost`
 * breaks the stack in ways that only show up at login.
 *
 * TLS needs no special handling: zero:tls derives its mkcert SANs from
 * GRAM_SITE_URL / GRAM_SERVER_URL and regenerates on every `./zero`, so the
 * remote hostname joins localhost and host.docker.internal on the same cert.
 *
 * `GRAM_DEV_HOSTNAME` is written first as a marker recording the intent. Ports are
 * randomized per worktree, so `git:workinit` re-emits the port-dependent vars
 * (GRAM_SITE_URL among them) into mise.local.toml AFTER copying it from the main
 * worktree — which would clobber these overrides. workinit therefore re-runs this
 * task at the end when the marker is present, so the hostname overrides always
 * land last and every new worktree inherits remote access for free.
 */

import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { parseTOML } from "confbox";

const LOCAL_FILE = "mise.local.toml";

/** Records the operator's intent so `git:workinit` can re-apply after remapping ports. */
const MARKER = "GRAM_DEV_HOSTNAME";

/** Hostname vars rewritten to the remote host; dependents are re-emitted after each. */
const HOST_VARS = ["GRAM_HOST", "GRAM_ADMIN_HOST"];

/**
 * Vars that must keep pointing at this box even though the hostname moved. Values
 * are mise templates so they track whatever ports this worktree was assigned.
 */
const PINNED_LOCAL: Record<string, string> = {
  GRAM_SERVER_BACKEND_URL: "https://localhost:{{env.GRAM_SERVER_PORT}}",
  // Derived from GRAM_ADMIN_HOST in mise.toml, so the dependent walk above
  // rewrites it — but mise.toml is explicit that only the admin dashboard's dev
  // proxy dials this and browsers never do, which makes it a devbox-local dial.
  // Pinned back afterwards; PINNED_LOCAL is applied last for exactly this.
  GRAM_ADMIN_BACKEND_URL: "https://localhost:{{env.GRAM_ADMIN_PORT}}",
};

/** Every var this task owns, for --reset and for clearing stale state before a re-run. */
const OWNED = [
  MARKER,
  ...HOST_VARS,
  "VITE_DEV_HOSTNAMES",
  "GRAM_IDP_BASE_URL",
  "MOCK_OIDC_BROWSER_BASE_URL",
  ...Object.keys(PINNED_LOCAL),
];

function mise(...args: string[]): string {
  return execFileSync("mise", args, { encoding: "utf-8" });
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

/** Reads the tailnet MagicDNS name from whichever tailscaled this user can talk to. */
function detectHost(): string {
  const sockets = [
    process.env["TAILSCALE_SOCKET"],
    `${process.env["HOME"]}/.tailscale/tailscaled.sock`,
    undefined, // system daemon at its default path
  ];

  for (const socket of sockets) {
    if (socket === null) continue;
    const args = socket
      ? ["--socket", socket, "status", "--json"]
      : ["status", "--json"];
    let raw: string;
    try {
      raw = execFileSync("tailscale", args, {
        encoding: "utf-8",
        stdio: ["ignore", "pipe", "ignore"],
      });
    } catch {
      continue;
    }
    const name = JSON.parse(raw)?.Self?.DNSName;
    // MagicDNS names come back fully qualified with a trailing dot.
    if (typeof name === "string" && name.length > 1)
      return name.replace(/\.$/, "");
  }

  throw new Error(
    "Could not read a hostname from tailscale. Is the daemon logged in? Pass --host explicitly instead.",
  );
}

/**
 * Vars whose value references `varName`, transitively. Mirrors the walk in
 * zero:remap-ports — the same precedence trap applies to hostnames.
 */
function findDependentEnvVars(
  config: Record<string, string>,
  varName: string,
): [string, string][] {
  const dependents: [string, string][] = [];
  for (const [key, value] of Object.entries(config)) {
    if (typeof value !== "string") continue;
    if (value.includes(varName)) {
      dependents.push([key, value]);
      dependents.push(...findDependentEnvVars(config, key));
    }
  }
  return dependents;
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

function main(): void {
  if (process.env["usage_reset"] === "true") {
    for (const key of OWNED) unset(key);
    console.log(
      "✅ Reverted to localhost. Re-run `./zero` to regenerate certs and restart.",
    );
    return;
  }

  const flagHost = process.env["usage_host"]?.trim();
  if (flagHost && process.env["usage_detect"] === "true") {
    throw new Error("Pass either --host or --detect, not both.");
  }

  // With no flags, re-apply whatever is already recorded. This is the path
  // `git:workinit` takes on a fresh worktree.
  const host =
    flagHost ||
    (process.env["usage_detect"] === "true" ? detectHost() : currentMarker());
  if (!host) {
    throw new Error(
      "No hostname configured. Run with --detect (reads it from tailscale) or --host <name>.",
    );
  }

  const config = parseTOML(readFileSync("mise.toml", "utf-8")) as {
    env: Record<string, string>;
  };

  // Clear everything first so a re-run with a different host cannot leave a
  // stale override sitting after the vars it should have been replaced by.
  for (const key of OWNED) unset(key);

  set(MARKER, host);

  for (const hostVar of HOST_VARS) {
    set(hostVar, host);
    for (const [key, value] of findDependentEnvVars(config.env, hostVar)) {
      set(key, value);
    }
  }

  // Vite rejects unknown Host headers. "devbox" is already in the config's
  // built-in allowlist; add the fully qualified name and keep localhost working
  // for anything still dialing this box directly.
  set("VITE_DEV_HOSTNAMES", `localhost,devbox,${host}`);

  // Browser-facing half of the dev-idp split (see the header comment). The
  // server-side half stays on localhost via GRAM_DEVIDP_HOST, untouched.
  set("GRAM_IDP_BASE_URL", `http://${host}:{{env.GRAM_DEVIDP_PORT}}/oauth2`);

  // Same split for the admin dashboard's fake Google IdP: only the endpoint the
  // browser is redirected to moves. GRAM_ADMIN_OIDC_EMULATOR_URL — the discovery
  // URL the admin server fetches, and the issuer it verifies — stays local.
  set(
    "MOCK_OIDC_BROWSER_BASE_URL",
    `http://${host}:{{env.GRAM_ADMIN_OIDC_EMULATOR_PORT}}`,
  );

  for (const [key, value] of Object.entries(PINNED_LOCAL)) {
    set(key, value);
  }

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
  console.log();
  console.log(`  3. From that same machine, check it:`);
  console.log(`       curl -k https://${host}:${sitePort}/`);
  console.log();
  console.log("docs/remote-dev-access.md covers the rest.");
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
