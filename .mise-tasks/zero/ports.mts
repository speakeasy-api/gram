#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE dir="{{ config_root }}"
//MISE hide=true

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

  let env = "[env]\n";
  env += finalVars.map(([key, value]) => `${key} = "${value}"`).join("\n");
  env += "\n";

  writeFileSync("mise.worktree.local.toml", env);
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
