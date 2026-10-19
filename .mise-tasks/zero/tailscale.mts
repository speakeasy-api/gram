#!/usr/bin/env node

//MISE description="Configure local development on this machine's Tailscale address"
//MISE dir="{{ config_root }}"
//USAGE flag "--force" help="Run setup even when GRAM_ZERO_SKIP includes zero:tailscale or *."
//USAGE flag "--skip <tasks>" env="GRAM_ZERO_SKIP" default="" help="Comma-separated onboarding tasks to skip."

import { execFileSync, spawnSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import {
  chmodSync,
  lstatSync,
  readFileSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { isIP } from "node:net";
import { join } from "node:path";
import { confirm, isCancel } from "@clack/prompts";
import { parseTOML } from "confbox";

interface TailscaleStatus {
  BackendState?: string;
  TailscaleIPs?: string[];
  Self?: { DNSName?: string; TailscaleIPs?: string[] };
}

interface SetupOptions {
  root?: string;
  env?: NodeJS.ProcessEnv;
  prompt?: (hostname: string, address: string) => Promise<boolean | symbol>;
}

function commaList(value: string | undefined): string[] {
  return (value ?? "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

function skip(reason: string): void {
  console.log(reason);
  console.log("Run `mise run zero:tailscale --force` to run this setup again.");
}

function localSnapshot(
  path: string,
): { contents: string; mode: number } | undefined {
  try {
    const stat = lstatSync(path);
    if (!stat.isFile() || stat.nlink !== 1) {
      throw new Error(
        "Tailscale setup requires a regular mise.local.toml, not a link.",
      );
    }
    return { contents: readFileSync(path, "utf8"), mode: stat.mode & 0o777 };
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return undefined;
    throw error;
  }
}

function saveSettings(
  root: string,
  env: NodeJS.ProcessEnv,
  settings: [string, string][],
  original: ReturnType<typeof localSnapshot>,
): void {
  const localPath = join(root, "mise.local.toml");
  // This name is gitignored, and staging stays on the same filesystem for rename.
  const stagingPath = join(root, `mise.tailscale-${randomUUID()}.local.toml`);
  writeFileSync(stagingPath, original?.contents ?? "", {
    flag: "wx",
    mode: 0o600,
  });
  try {
    // Reinsert templates in dependency order without exposing an intermediate file.
    if (original) {
      execFileSync(
        "mise",
        ["unset", "--file", stagingPath, ...settings.map(([key]) => key)],
        { cwd: root, env },
      );
    }
    execFileSync(
      "mise",
      [
        "set",
        "--file",
        stagingPath,
        ...settings.map(([key, value]) => `${key}=${value}`),
      ],
      { cwd: root, env },
    );
    // Best-effort only: plain mise commands and editors share no writer lock.
    // They can still change the file between this comparison and the rename.
    const current = localSnapshot(localPath);
    if (
      current?.contents !== original?.contents ||
      current?.mode !== original?.mode
    ) {
      throw new Error(
        "mise.local.toml changed during Tailscale setup; rerun setup to keep those edits.",
      );
    }
    chmodSync(stagingPath, original?.mode ?? 0o600);
    renameSync(stagingPath, localPath);
  } finally {
    rmSync(stagingPath, { force: true });
  }
}

export function dependentEnvVars(
  config: Record<string, unknown>,
  roots: string[],
): [string, string][] {
  const dependencies = new Map<string, string[]>();
  for (const [key, value] of Object.entries(config)) {
    if (typeof value !== "string") continue;
    dependencies.set(
      key,
      [...value.matchAll(/\benv\.([A-Za-z_][A-Za-z0-9_]*)\b/g)].map(
        (match) => match[1],
      ),
    );
  }

  const affected = new Set(roots);
  let changed = true;
  while (changed) {
    changed = false;
    for (const [key, refs] of dependencies) {
      if (!affected.has(key) && refs.some((ref) => affected.has(ref))) {
        affected.add(key);
        changed = true;
      }
    }
  }

  const visited = new Set(roots);
  const visiting = new Set<string>();
  const ordered: [string, string][] = [];
  function visit(key: string): void {
    if (visited.has(key)) return;
    if (visiting.has(key))
      throw new Error(`Cyclic environment dependency: ${key}`);
    visiting.add(key);
    for (const ref of dependencies.get(key) ?? []) {
      if (affected.has(ref)) visit(ref);
    }
    visiting.delete(key);
    visited.add(key);
    ordered.push([key, config[key] as string]);
  }
  for (const key of dependencies.keys()) {
    if (affected.has(key)) visit(key);
  }
  return ordered;
}

export async function setupTailscale(
  options: SetupOptions = {},
): Promise<void> {
  const root = options.root ?? process.cwd();
  const env = options.env ?? process.env;
  const force = env["usage_force"] === "true";
  const skipped = commaList(env["usage_skip"] ?? env["GRAM_ZERO_SKIP"]);
  if (!force && (skipped.includes("*") || skipped.includes("zero:tailscale"))) {
    skip("Tailscale setup is disabled by GRAM_ZERO_SKIP.");
    return;
  }

  const result = spawnSync("tailscale", ["status", "--json", "--peers=false"], {
    cwd: root,
    // The macOS app executable also provides the CLI; do not open its GUI.
    env: { ...env, TAILSCALE_BE_CLI: "1" },
    encoding: "utf8",
    timeout: 10_000,
  });
  if (result.error || result.status !== 0) {
    skip(
      "Tailscale is unavailable. Install its CLI on PATH, start it, and log in first.",
    );
    return;
  }
  let status: TailscaleStatus | null;
  try {
    status = JSON.parse(result.stdout) as TailscaleStatus | null;
  } catch {
    skip("Could not read Tailscale status. Check that Tailscale is running.");
    return;
  }
  if (status?.BackendState !== "Running") {
    skip("Tailscale is not connected. Start it and log in first.");
    return;
  }
  const dnsName = status.Self?.DNSName;
  const hostname =
    typeof dnsName === "string" ? dnsName.trim().replace(/\.+$/, "") : "";
  if (!hostname) {
    skip(
      "This machine has no Tailnet DNS name. Check the Tailnet's MagicDNS configuration.",
    );
    return;
  }
  const addresses = status.Self?.TailscaleIPs ?? status.TailscaleIPs;
  const ips = Array.isArray(addresses)
    ? addresses.filter((ip) => typeof ip === "string" && isIP(ip) !== 0)
    : [];
  const address = ips.find((ip) => isIP(ip) === 4) ?? ips[0];
  if (!address) {
    skip(
      "This machine has no Tailscale IP address. Check its Tailscale connection.",
    );
    return;
  }
  const localPath = join(root, "mise.local.toml");
  const original = localSnapshot(localPath);
  const local = original
    ? (parseTOML(original.contents) as { env?: Record<string, string> })
    : {};
  const config = parseTOML(readFileSync(join(root, "mise.toml"), "utf8")) as {
    env: Record<string, unknown>;
  };
  const bindAddress = isIP(address) === 6 ? `[${address}]` : address;
  const dependents = [
    ...dependentEnvVars(config.env, ["GRAM_HOST"]),
    ...dependentEnvVars(config.env, ["GRAM_BIND_IP"]).map(
      ([key, value]): [string, string] => {
        const saved = local.env?.[key];
        // Templates follow the shared config; literal listener overrides are
        // developer-owned and remain intact when setup is refreshed.
        return [
          key,
          saved !== undefined && !saved.includes("{{") ? saved : value,
        ];
      },
    ),
  ];
  const savedHosts = new Set(commaList(local.env?.["GRAM_SSL_EXTRA_HOSTS"]));
  if (
    !force &&
    local.env?.["GRAM_HOST"] === hostname &&
    local.env?.["GRAM_BIND_IP"] === bindAddress &&
    [hostname, ...ips].every((host) => savedHosts.has(host)) &&
    dependents.every(([key, value]) => local.env?.[key] === value)
  ) {
    skip(
      "Tailscale development settings are already configured for this machine.",
    );
    return;
  }
  if (!options.prompt && !process.stdin.isTTY) {
    skip("Tailscale setup needs an interactive terminal.");
    return;
  }
  const answer = await (
    options.prompt ??
    ((host, ip) =>
      confirm({
        message: `Configure local development for ${host} over Tailscale and bind public services to ${ip}?`,
        initialValue: false,
      }))
  )(hostname, address);
  if (isCancel(answer) || typeof answer !== "boolean") {
    skip("Tailscale setup cancelled.");
    return;
  }

  if (!answer) {
    const values = new Set([
      ...commaList(local.env?.["GRAM_ZERO_SKIP"]),
      ...skipped,
      "zero:tailscale",
    ]);
    execFileSync(
      "mise",
      [
        "set",
        "--file",
        "mise.local.toml",
        `GRAM_ZERO_SKIP=${[...values].join(",")}`,
      ],
      { cwd: root, env },
    );
    skip("Tailscale setup declined; saved zero:tailscale in GRAM_ZERO_SKIP.");
    return;
  }

  const settings: [string, string][] = [
    ["GRAM_HOST", hostname],
    ["GRAM_BIND_IP", bindAddress],
    ...dependents,
    [
      "GRAM_SSL_EXTRA_HOSTS",
      [
        ...new Set([
          ...commaList(
            local.env?.["GRAM_SSL_EXTRA_HOSTS"] ?? env["GRAM_SSL_EXTRA_HOSTS"],
          ),
          hostname,
          ...ips,
        ]),
      ].join(","),
    ],
  ];
  saveSettings(root, env, settings, original);
  console.log("Tailscale development settings saved in mise.local.toml.");
  console.log(
    "Run `mise run zero:tls` to refresh certificates; ./zero runs it next.",
  );
}

if (import.meta.main) {
  try {
    await setupTailscale();
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
