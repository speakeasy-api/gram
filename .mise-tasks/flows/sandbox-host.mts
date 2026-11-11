#!/usr/bin/env node

//MISE description="Serve this worktree's stack on another host name, for an agent sandboxed in a container on the same machine (flow-checks CI)"
//MISE dir="{{ config_root }}"
//USAGE arg "<host>" help="The name the sandbox reaches this machine by, e.g. host.docker.internal"
//USAGE flag "--bind-ip <ip>" required=#true help="Address the server and dev-idp listen on, so the sandbox can reach them. No default: on a shared machine, choose deliberately"
//USAGE flag "--dry-run" help="Print the settings instead of saving them"

// The flow-checks heal agent runs in GitHub Agentic Workflows' AWF sandbox, a
// container on the runner. Inside it, `localhost` is the container, so the
// stack has to be served on the name the container reaches the runner by,
// and listen on an address that accepts traffic from the container network.
//
// Only the services the sandbox opens move to the bind address: the server
// and dev-idp (the dashboard follows GRAM_BIND_IP on its own). The admin API
// and its mock OIDC provider stay on loopback, as the health and control
// listeners always do. A developer's own literal listener setting in
// mise.local.toml is kept, as zero:tailscale keeps it.
//
// Run before `./zero`: it writes mise.local.toml, then zero boots the stack
// with those settings and issues a TLS certificate that covers the host.
// Uses only Node built-ins, since the workspace's packages are not installed
// yet at that point.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { isIP } from "node:net";
import { dependentEnvVars } from "../zero/env-deps.mts";

const ROOTS = ["GRAM_HOST", "GRAM_BIND_IP"];
// Listeners the sandboxed agent reaches. Every other GRAM_BIND_IP listener
// stays on loopback.
const SANDBOX_LISTENERS = new Set([
  "GRAM_SERVER_ADDRESS",
  "GRAM_DEVIDP_ADDRESS",
]);

// The [env] table of mise.toml: string values, and the `default` of an
// inline `{ default = "..." }` table. Anything else that mentions a root is
// an error rather than a silent miss.
export function readEnv(toml: string): Record<string, string> {
  const env: Record<string, string> = {};
  let inEnv = false;
  for (const raw of toml.split("\n")) {
    const line = raw.trim();
    if (line.startsWith("[")) {
      inEnv = line === "[env]";
      continue;
    }
    if (!inEnv || line === "" || line.startsWith("#")) continue;
    const m =
      /^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(?:"((?:[^"\\]|\\.)*)"|\{\s*default\s*=\s*"((?:[^"\\]|\\.)*)"\s*\})\s*(?:#.*)?$/.exec(
        line,
      );
    if (m) {
      env[m[1]!] = m[2] ?? m[3]!;
    } else if (ROOTS.some((root) => line.includes(`env.${root}`))) {
      throw new Error(`Cannot read this mise.toml line: ${line}`);
    }
  }
  return env;
}

function commaList(value: string | undefined): string[] {
  return (value ?? "")
    .split(",")
    .map((v) => v.trim())
    .filter(Boolean);
}

export function validHost(host: string): boolean {
  return (
    host.length <= 253 &&
    /^(?=.{1,253}$)([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$/.test(
      host,
    )
  );
}

// The settings to save in mise.local.toml, in order: the roots first, then
// every template that depends on them, then the lists that must include the
// host (the TLS certificate's names and the dashboard's allowed hosts).
export function sandboxSettings(
  env: Record<string, string>,
  host: string,
  bindIp: string,
  local: Record<string, string> = {},
): [string, string][] {
  if (!validHost(host)) throw new Error(`Not a host name: ${host}`);
  if (isIP(bindIp) === 0) throw new Error(`Not an IP address: ${bindIp}`);
  const bind = isIP(bindIp) === 6 ? `[${bindIp}]` : bindIp;
  const withHost = (key: string) =>
    [...new Set([...commaList(local[key] ?? env[key]), host])].join(",");
  const readsBindIp = new Set(
    dependentEnvVars(env, ["GRAM_BIND_IP"]).map(([key]) => key),
  );
  const dependents = dependentEnvVars(env, ROOTS).map(
    ([key, value]): [string, string] => {
      if (!readsBindIp.has(key)) return [key, value];
      // A literal listener the developer set is theirs to keep.
      const saved = local[key];
      if (saved !== undefined && !saved.includes("{{")) return [key, saved];
      if (SANDBOX_LISTENERS.has(key)) return [key, value];
      return [key, value.replaceAll("{{env.GRAM_BIND_IP}}", "127.0.0.1")];
    },
  );
  return [
    ["GRAM_HOST", host],
    ["GRAM_BIND_IP", bind],
    ...dependents,
    ["GRAM_SSL_EXTRA_HOSTS", withHost("GRAM_SSL_EXTRA_HOSTS")],
    ["VITE_DEV_HOSTNAMES", withHost("VITE_DEV_HOSTNAMES")],
  ];
}

if (import.meta.main) {
  try {
    const host = process.env["usage_host"] ?? "";
    const bindIp = process.env["usage_bind_ip"] ?? "";
    const local = existsSync("mise.local.toml")
      ? readEnv(readFileSync("mise.local.toml", "utf8"))
      : {};
    const settings = sandboxSettings(
      readEnv(readFileSync("mise.toml", "utf8")),
      host,
      bindIp,
      local,
    );
    if (process.env["usage_dry_run"] === "true") {
      for (const [k, v] of settings) console.log(`${k}=${v}`);
    } else {
      execFileSync(
        "mise",
        [
          "set",
          "--file",
          "mise.local.toml",
          ...settings.map(([k, v]) => `${k}=${v}`),
        ],
        { stdio: "inherit" },
      );
      console.log(`The stack will serve on ${host}, listening on ${bindIp}.`);
    }
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
