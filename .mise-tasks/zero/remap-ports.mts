#!/usr/bin/env -S node

//MISE dir="{{ config_root }}"
//MISE hide=true
//MISE description="Finds available ports for any environment variables ending with `_PORT` in the `mise.toml` file and writes them to a new `mise.worktree.local.toml` file."

//USAGE flag "--format <format>" default="mise" { choices "mise" "flat" }
//USAGE flag "--file <file>" default="mise.worktree.local.toml" help="The file to write the environment variables to. If set to '-', the output will be written to stdout."
//USAGE flag "--preserve" help="Preserve existing port assignments and dependent declarations already present in mise.local.toml. Only emit newly-introduced ports (randomized) and newly-introduced dependent declarations."

/**
 * This script is responsible for finding available ports for any environment
 * variables ending with `_PORT` in the `mise.toml` file and writing them to a
 * new env var config file. The output format (mise or flat) and destination
 * file are configurable via flags, with support for writing to stdout. Any
 * environment variables that depend on the `_PORT` variables will also need to
 * be picked up and redeclared since env var declarations are sensitive to
 * config loading precedence and order dependent within each config file.
 *
 * When `--preserve` is set the script reads `mise.local.toml` and skips any
 * `_PORT` or dependent declaration that already has a value there. This is
 * what `mise git:worksync` (alias `gws`) uses to bring an existing worktree
 * up to date with new ports / dependents added on `main` without
 * re-randomizing ports that are already assigned and without clobbering
 * manual edits the user may have made to dependent values.
 */

import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { parseTOML } from "confbox";
import { checkPort } from "get-port-please";

/**
 * Ports of services that are shared across ALL worktrees (see
 * compose.shared.yml). These must NOT be remapped: every worktree reaches the
 * single shared stack on the same default host port. Skipping a port here also
 * skips any env var that depends on it (e.g. PRESIDIO_ANALYZER_URL), so those
 * keep their mise.toml defaults too.
 */
const SHARED_PORT_ENV_VARS = new Set([
  "CLICKHOUSE_HTTP_PORT",
  "CLICKHOUSE_NATIVE_PORT",
  "PRESIDIO_PORT",
  "PUBSUB_EMULATOR_PORT",
  "TEMPORAL_PORT",
  "TEMPORAL_WEB_PORT",
  // Temporal and the LGTM stack are shared, so every worktree must reach them
  // on the same default host ports. TEMPORAL_ADDRESS and
  // OTEL_EXPORTER_OTLP_ENDPOINT are derived from skipped ports and therefore
  // keep their mise.toml defaults too.
  "GRAFANA_PORT",
  "TEMPO_HTTP_PORT",
  "LOKI_HTTP_PORT",
  "PROMETHEUS_PORT",
  "OTLP_GRPC_PORT",
  "OTLP_HTTP_PORT",
]);

/**
 * Range to draw worktree ports from. Deliberately BELOW the ephemeral range
 * (49152-65535 on macOS and Linux): a port picked there is one the kernel also
 * hands out to outbound sockets, and nothing holds the assignment between
 * `git:workinit` picking it and Docker binding it minutes later during the
 * boot. When that race is lost the whole stack dies at `infra:start` with
 * `failed to bind host port ...: address already in use`, and the worktree
 * never seeds. Stay above 20000 to clear the fixed ports in `mise.toml`.
 */
const PORT_RANGE_START = 20_000;
const PORT_RANGE_END = 49_151;

/** Attempts per port before giving up; the range holds ~29k candidates. */
const MAX_PORT_ATTEMPTS = 100;

/**
 * Ports already spoken for by another worktree. A sibling's ports are recorded
 * in its `mise.local.toml` as soon as `git:workinit` runs, but are not bound
 * until its stack boots, so probing the host alone cannot see them: creating
 * two worktrees back to back would otherwise hand both the same ports and the
 * second stack would fail to bind. Best-effort — a worktree we cannot read
 * just does not contribute reservations.
 */
function reservedByOtherWorktrees(): Set<number> {
  const reserved = new Set<number>();

  let porcelain: string;
  try {
    porcelain = execFileSync("git", ["worktree", "list", "--porcelain"], {
      encoding: "utf-8",
    });
  } catch {
    return reserved;
  }

  for (const line of porcelain.split("\n")) {
    if (!line.startsWith("worktree ")) continue;
    const dir = line.slice("worktree ".length).trim();
    let local: { env?: Record<string, string> };
    try {
      local = parseTOML(readFileSync(join(dir, "mise.local.toml"), "utf-8"));
    } catch {
      continue;
    }
    for (const [key, value] of Object.entries(local.env ?? {})) {
      if (!key.endsWith("_PORT")) continue;
      const port = Number(value);
      if (Number.isInteger(port)) reserved.add(port);
    }
  }

  return reserved;
}

/**
 * Picks a free port in the non-ephemeral range that no other worktree has
 * claimed, recording it in `reserved` so the rest of this run skips it too.
 */
async function allocatePort(reserved: Set<number>): Promise<number> {
  const span = PORT_RANGE_END - PORT_RANGE_START + 1;
  for (let attempt = 0; attempt < MAX_PORT_ATTEMPTS; attempt++) {
    const candidate = PORT_RANGE_START + Math.floor(Math.random() * span);
    if (reserved.has(candidate)) continue;
    // checkPort with no host tries every local address plus 0.0.0.0, which is
    // what Docker publishes on.
    if ((await checkPort(candidate)) === false) continue;
    reserved.add(candidate);
    return candidate;
  }
  throw new Error(
    `Unable to find a free port in ${PORT_RANGE_START}-${PORT_RANGE_END} after ${MAX_PORT_ATTEMPTS} attempts`,
  );
}

async function main() {
  const config = parseTOML(await readFileSync("mise.toml", "utf-8")) as {
    env: Record<string, string>;
  };

  const preserve = process.env["usage_preserve"] === "true";

  let existing: Record<string, string> = {};
  if (preserve) {
    try {
      const localConfig = parseTOML(
        await readFileSync("mise.local.toml", "utf-8"),
      ) as { env?: Record<string, string> };
      existing = localConfig.env ?? {};
    } catch {
      // mise.local.toml is missing — treat as empty and emit everything.
    }
  }

  const portEnvVars = Object.keys(config.env).filter(
    (key) => key.endsWith("_PORT") && !SHARED_PORT_ENV_VARS.has(key),
  );

  // Ports this worktree keeps (--preserve) are reserved too, so a newly-added
  // _PORT var cannot be handed a port this worktree already uses.
  const reserved = reservedByOtherWorktrees();
  for (const [key, value] of Object.entries(existing)) {
    if (!key.endsWith("_PORT")) continue;
    const port = Number(value);
    if (Number.isInteger(port)) reserved.add(port);
  }

  const emitted = new Map<string, string>();
  const emit = (key: string, value: string) => {
    // delete-then-set moves the key to the end of insertion order, matching
    // the unset+set semantics of `mise set` so dependents end up after the
    // latest port they reference.
    emitted.delete(key);
    emitted.set(key, value);
  };

  for (const portEnvVar of portEnvVars) {
    if (preserve && portEnvVar in existing) {
      // Port is already assigned in mise.local.toml — keep it.
    } else {
      emit(portEnvVar, `${await allocatePort(reserved)}`);
    }

    for (const [key, value] of findDependentEnvVars(config.env, portEnvVar)) {
      if (preserve && key in existing) continue;
      emit(key, value);
    }
  }

  const finalVars = Array.from(emitted.entries());

  const format = process.env["usage_format"] ?? "mise";
  let out = "";
  switch (format) {
    case "mise":
      out = "[env]\n";
      out += finalVars.map(([key, value]) => `${key} = "${value}"`).join("\n");
      out += "\n";
      break;
    case "flat":
      out = finalVars.map(([key, value]) => `${key}=${value}`).join("\n");
      break;
    default:
      throw new Error(`Unsupported format: ${process.env["usage_format"]}`);
  }

  const file = process.env["usage_file"] ?? "mise.worktree.local.toml";
  if (file === "-") {
    console.log(out);
  } else {
    writeFileSync(file, out);
  }
}

function findDependentEnvVars(
  config: Record<string, string>,
  varName: string,
): [string, string][] {
  const dependentEnvVars: [string, string][] = [];
  for (const [key, value] of Object.entries(config)) {
    if (typeof value !== "string") continue;

    if (value.includes(varName)) {
      dependentEnvVars.push([key, value]);
      dependentEnvVars.push(...findDependentEnvVars(config, key));
    }
  }
  return dependentEnvVars;
}

main();
