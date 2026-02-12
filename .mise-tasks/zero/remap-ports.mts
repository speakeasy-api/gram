#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE dir="{{ config_root }}"
//MISE hide=true
//MISE description="Finds available ports for any environment variables ending with `_PORT` in the `mise.toml` file and writes them to a new `mise.worktree.local.toml` file."

//USAGE flag "--format <format>" default="mise" { choices "mise" "flat" }
//USAGE flag "--file <file>" default="mise.worktree.local.toml" help="The file to write the environment variables to. If set to '-', the output will be written to stdout."

/**
 * This script is responsible for finding available ports for any environment
 * variables ending with `_PORT` in the `mise.toml` file and writing them to a
 * new env var config file. The output format (mise or flat) and destination
 * file are configurable via flags, with support for writing to stdout. Any
 * environment variables that depend on the `_PORT` variables will also need to
 * be picked up and redeclared since env var declarations are sensitive to
 * config loading precedence and order dependent within each config file.
 */

import { readFileSync, writeFileSync } from "node:fs";
import { parseTOML } from "confbox";
import { getPort } from "get-port-please";

async function main() {
  const config = parseTOML(await readFileSync("mise.toml", "utf-8")) as {
    env: Record<string, string>;
  };

  const portEnvVars = Object.keys(config.env).filter((key) =>
    key.endsWith("_PORT"),
  );

  const finalVars: [string, string][] = [];

  for (const portEnvVar of portEnvVars) {
    const port = await getPort({
      name: portEnvVar,
      random: true,
    });
    finalVars.push(
      [portEnvVar, `${port}`],
      ...findDependentEnvVars(config.env, portEnvVar),
    );
  }

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
