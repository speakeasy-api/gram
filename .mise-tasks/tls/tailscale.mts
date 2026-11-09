#!/usr/bin/env node

//MISE description="Fetch or renew this machine's Tailscale HTTPS certificate for local TLS"
//MISE dir="{{ config_root }}"
//USAGE flag "--domain <domain>" env="GRAM_HOST" help="Tailnet DNS name to fetch the certificate for."
//USAGE flag "--min-validity <duration>" default="336h" help="Renew when the certificate expires sooner than this."
//USAGE flag "--restart" help="Restart the daemons that serve TLS when the certificate changed."

// Tailscale issues a Let's Encrypt certificate for the machine's Tailnet DNS
// name, which browsers on other tailnet devices trust without any local CA.
// tailscaled caches it and serialises issuance per domain, so every worktree
// can call this concurrently: an unexpired certificate is a local read.
//
// The certificate and key change together on renewal (each issuance gets a new
// key), so they are written into a fresh directory and published by renaming
// the `local/ssl/tailscale` symlink. A daemon starting at any moment reads a
// matching pair through that link.

import { createPrivateKey, randomUUID, X509Certificate } from "node:crypto";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  renameSync,
  rmSync,
  statSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { basename, join } from "node:path";
import { $ } from "zx";

/** Daemons whose listeners load GRAM_SSL_CERT_FILE at startup. */
export const TLS_DAEMONS = [
  "dev-idp",
  "admin",
  "admin-dashboard",
  "server",
  "dashboard",
];

const LINK = "tailscale";
const VERSION_PREFIX = "tailscale-";
// Superseded versions are kept briefly: a concurrent run may have just created
// one and not yet linked it, and a daemon may be mid-read of the previous one.
const STALE_AFTER_MS = 60 * 60 * 1000;

export interface CertPair {
  cert: string;
  key: string;
}

/** Splits `tailscale cert --cert-file - --key-file -` output into its parts. */
export function splitPEM(output: string): CertPair {
  const keyBlock =
    /-----BEGIN ([A-Z ]*)PRIVATE KEY-----[\s\S]*?-----END \1PRIVATE KEY-----\n?/;
  const match = output.match(keyBlock);
  if (!match) throw new Error("Tailscale returned no private key.");
  const key = match[0].endsWith("\n") ? match[0] : `${match[0]}\n`;
  const cert = output.replace(keyBlock, "").trim();
  if (!cert.includes("-----BEGIN CERTIFICATE-----")) {
    throw new Error("Tailscale returned no certificate.");
  }
  const pair = { cert: `${cert}\n`, key };
  if (!new X509Certificate(pair.cert).checkPrivateKey(createPrivateKey(key))) {
    throw new Error(
      "Tailscale returned a certificate and key that do not match.",
    );
  }
  return pair;
}

function readCurrent(sslDir: string): CertPair | undefined {
  try {
    return {
      cert: readFileSync(join(sslDir, LINK, "cert.pem"), "utf8"),
      key: readFileSync(join(sslDir, LINK, "key.pem"), "utf8"),
    };
  } catch {
    return undefined;
  }
}

function removeStaleVersions(sslDir: string, current: string): void {
  const now = Date.now();
  for (const name of readdirSync(sslDir)) {
    if (!name.startsWith(VERSION_PREFIX) || name === current) continue;
    const path = join(sslDir, name);
    try {
      if (now - statSync(path).mtimeMs > STALE_AFTER_MS) {
        rmSync(path, { recursive: true, force: true });
      }
    } catch {
      // Another run removed it first.
    }
  }
}

/**
 * Publishes the pair under `<sslDir>/tailscale/{cert,key}.pem`. Returns false
 * without touching the filesystem when the published pair is already current.
 */
export function publish(sslDir: string, pair: CertPair): boolean {
  const current = readCurrent(sslDir);
  if (current?.cert === pair.cert && current.key === pair.key) return false;

  mkdirSync(sslDir, { recursive: true });
  const version = mkdtempSync(join(sslDir, VERSION_PREFIX));
  writeFileSync(join(version, "cert.pem"), pair.cert, { mode: 0o644 });
  writeFileSync(join(version, "key.pem"), pair.key, { mode: 0o600 });
  const staging = join(sslDir, `.${LINK}-${randomUUID()}`);
  symlinkSync(basename(version), staging);
  // rename(2) replaces the old link atomically on Linux and macOS.
  renameSync(staging, join(sslDir, LINK));
  removeStaleVersions(sslDir, basename(version));
  return true;
}

export async function fetchPair(
  domain: string,
  minValidity: string,
  env: NodeJS.ProcessEnv = process.env,
): Promise<CertPair> {
  // Output goes to stdout because the sandboxed macOS CLI redirects file
  // writes into its container.
  const result = await $({
    env: { ...env, TAILSCALE_BE_CLI: "1" },
    nothrow: true,
    quiet: true,
    timeout: "120s",
  })`tailscale cert --min-validity ${minValidity} --cert-file - --key-file - ${domain}`;
  if (result.exitCode !== 0) {
    const detail = result.stderr.trim();
    throw new Error(
      [
        `tailscale cert failed for ${domain}${detail ? `: ${detail}` : "."}`,
        "Check that HTTPS certificates are enabled for the tailnet (admin console, DNS page)",
        "and, on Linux, that this user is the Tailscale operator: sudo tailscale set --operator=$USER",
      ].join("\n"),
    );
  }
  return splitPEM(result.stdout);
}

/** True while a pause, wake or start owns this worktree's stack. */
async function stackBusy(root: string): Promise<boolean> {
  const result = await $({
    cwd: root,
    nothrow: true,
    quiet: true,
  })`git rev-parse --absolute-git-dir`;
  if (result.exitCode !== 0) return true;
  const gitdir = result.stdout.trim();
  return (
    existsSync(join(gitdir, "gram-stack-lock")) ||
    existsSync(join(gitdir, "gram-stack-paused"))
  );
}

async function main(): Promise<void> {
  const root = process.cwd();
  const domain = (process.env["usage_domain"] ?? "").trim();
  if (!domain) throw new Error("--domain (or GRAM_HOST) is required.");
  const minValidity = process.env["usage_min_validity"] || "336h";
  const sslDir = join(root, "local", "ssl");

  const pair = await fetchPair(domain, minValidity);
  const expires = new X509Certificate(pair.cert).validTo;
  if (!publish(sslDir, pair)) {
    console.log(
      `Tailscale certificate for ${domain} is current (expires ${expires}).`,
    );
    return;
  }
  console.log(
    `Saved the Tailscale certificate for ${domain} (expires ${expires}).`,
  );

  if (process.env["usage_restart"] !== "true") return;
  // A lifecycle operation in progress starts the daemons itself, and they will
  // read the new certificate then.
  if (await stackBusy(root)) return;
  console.log(`Restarting ${TLS_DAEMONS.join(", ")} to load it.`);
  await $({ stdio: "inherit" })`pitchfork restart ${TLS_DAEMONS}`;
}

if (import.meta.main) {
  try {
    await main();
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
