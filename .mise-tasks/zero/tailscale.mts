#!/usr/bin/env node

//MISE description="Configure local development on this machine's Tailscale address"
//MISE dir="{{ config_root }}"
//USAGE flag "--force" help="Run setup even when GRAM_ZERO_SKIP includes zero:tailscale or *."
//USAGE flag "--skip <tasks>" env="GRAM_ZERO_SKIP" default="" help="Comma-separated onboarding tasks to skip."

import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
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
  const skipped = commaList(env["usage_skip"] ?? env["GRAM_ZERO_SKIP"]);
  if (
    env["usage_force"] !== "true" &&
    (skipped.includes("*") || skipped.includes("zero:tailscale"))
  ) {
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
  if (!options.prompt && !process.stdin.isTTY) {
    skip("Tailscale setup needs an interactive terminal.");
    return;
  }
  const answer = await (
    options.prompt ??
    ((host, ip) =>
      confirm({
        message: `Configure local development for ${host} (${ip}) over Tailscale?`,
        initialValue: false,
      }))
  )(hostname, address);
  if (isCancel(answer) || typeof answer !== "boolean") {
    skip("Tailscale setup cancelled.");
    return;
  }

  const localPath = join(root, "mise.local.toml");
  const local = existsSync(localPath)
    ? (parseTOML(readFileSync(localPath, "utf8")) as {
        env?: Record<string, string>;
      })
    : {};
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

  const config = parseTOML(readFileSync(join(root, "mise.toml"), "utf8")) as {
    env: Record<string, unknown>;
  };
  const settings: [string, string][] = [
    ["GRAM_HOST", hostname],
    ...dependentEnvVars(config.env, ["GRAM_HOST"]),
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
  // mise set preserves existing key positions. Reinsert affected settings so
  // templates follow their dependencies while keeping port pins and other settings.
  if (existsSync(localPath)) {
    execFileSync(
      "mise",
      ["unset", "--file", "mise.local.toml", ...settings.map(([key]) => key)],
      { cwd: root, env },
    );
  }
  execFileSync(
    "mise",
    [
      "set",
      "--file",
      "mise.local.toml",
      ...settings.map(([key, value]) => `${key}=${value}`),
    ],
    { cwd: root, env },
  );
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
