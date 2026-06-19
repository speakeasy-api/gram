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

import { readFileSync, writeFileSync } from "node:fs";
import { parseTOML } from "confbox";
import { getPort } from "get-port-please";

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

  const portEnvVars = Object.keys(config.env).filter((key) =>
    key.endsWith("_PORT"),
  );

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
      const port = await getPort({
        name: portEnvVar,
        random: true,
      });
      emit(portEnvVar, `${port}`);
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
