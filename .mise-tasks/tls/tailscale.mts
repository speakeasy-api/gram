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
// the `local/ssl/tailscale` symlink: the link always names a complete, matching
// pair. Daemons open the two files separately, so one starting at the instant
// of a swap could still read one file from each side; renewal is rare, and
// `--restart` restarts the TLS daemons after a swap, which covers that window.

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
import { reapStaleLock, releaseLock, tryCreateLock } from "../_stack-lock.mts";

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
// Daemons only reach a version through the link, and files they already opened
// survive deletion, so superseded versions can go as soon as the link moves.
// Recently created ones are kept: they may belong to a concurrent run that has
// written its version but not linked it yet.
const IN_FLIGHT_MS = 60 * 60 * 1000;

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
      if (now - statSync(path).mtimeMs > IN_FLIGHT_MS) {
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

/**
 * Restarts the TLS daemons while holding this worktree's stack lock, the same
 * symlink pause, wake and start take, so a restart cannot interleave with a
 * pause. Returns why it skipped, or undefined after restarting.
 */
export async function restartUnderStackLock(
  root: string,
  restart: () => Promise<unknown>,
): Promise<string | undefined> {
  const result = await $({
    cwd: root,
    nothrow: true,
    quiet: true,
  })`git rev-parse --absolute-git-dir`;
  if (result.exitCode !== 0) {
    return `could not find the git directory (${result.stderr.trim()})`;
  }
  const gitdir = result.stdout.trim();
  const lock = join(gitdir, "gram-stack-lock");
  const identity = `${process.pid}:unpause`;
  // A holder killed mid-operation (including an earlier renewal) leaves its
  // lock behind; clear it rather than skipping every restart until a wake.
  if (!(await tryCreateLock(lock, identity))) {
    await reapStaleLock(lock);
    if (!(await tryCreateLock(lock, identity))) {
      return "a pause, wake or start is in progress; it starts the daemons with the new certificate";
    }
  }
  try {
    if (existsSync(join(gitdir, "gram-stack-paused"))) {
      return "the stack is paused; wake loads the new certificate";
    }
    await restart();
    return undefined;
  } finally {
    await releaseLock(lock, identity);
  }
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
  const skipped = await restartUnderStackLock(root, async () => {
    console.log(`Restarting ${TLS_DAEMONS.join(", ")} to load it.`);
    await $({ stdio: "inherit" })`pitchfork restart ${TLS_DAEMONS}`;
  });
  if (skipped) {
    console.warn(
      `Did not restart the TLS daemons: ${skipped}. Running daemons keep the previous certificate until they restart.`,
    );
  }
}

if (import.meta.main) {
  try {
    await main();
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
