#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Configure and build the native local Code Mode runtime"
//MISE dir="{{ config_root }}"
//USAGE flag "--skip-build" help="Write missing local settings without building the runtime."

import { randomBytes } from "node:crypto";
import { existsSync, readFileSync, writeFileSync, chmodSync } from "node:fs";
import { join } from "node:path";
import { parseTOML } from "confbox";
import { $ } from "zx";

const filename = join(process.cwd(), "mise.local.toml");
let source = existsSync(filename) ? readFileSync(filename, "utf8") : "";
const config = parseTOML<{ env?: Record<string, unknown> }>(source);
const additions: string[] = [];

function ensureSetting(key: string, fallback: string): string {
  const saved = config.env?.[key];
  if (typeof saved === "string" && saved && saved !== "unset") return saved;
  // An existing non-string or empty entry needs an intentional operator edit.
  if (saved !== undefined) {
    throw new Error(`Set ${key} to a non-empty string in mise.local.toml.`);
  }
  const inherited = process.env[key];
  const value = inherited && inherited !== "unset" ? inherited : fallback;
  additions.push(`${key} = ${JSON.stringify(value)}`);
  return value;
}

const provider = ensureSetting("GRAM_CODE_RUNTIME_PROVIDER", "local");
if (provider === "local") {
  ensureSetting("GRAM_CODE_RUNTIME_TOKEN", randomBytes(32).toString("hex"));
}
if (additions.length) {
  if (!source.endsWith("\n")) source += "\n";
  const header = /^\[env\][ \t]*(?:#[^\n]*)?\n/m;
  if (header.test(source)) {
    source = source.replace(
      header,
      (match) => match + additions.join("\n") + "\n",
    );
  } else if (config.env === undefined) {
    source += "\n[env]\n" + additions.join("\n") + "\n";
  } else {
    throw new Error("Local configuration needs an [env] table.");
  }
  // Validate before writing; keep credentials out of command arguments and logs.
  parseTOML(source);
  writeFileSync(filename, source, { mode: 0o600 });
  chmodSync(filename, 0o600);
}
console.log("Code Mode settings ready in mise.local.toml.");

if (provider === "local" && process.env["usage_skip_build"] !== "true") {
  const common =
    await $`git rev-parse --path-format=absolute --git-common-dir`.quiet();
  // Cargo serializes shared builds and fingerprints source/toolchain changes.
  // Each worktree still gets its own installed executable. Cargo's install
  // metadata verifies the pinned revision even when an executable exists.
  await $({
    stdio: "inherit",
    env: {
      ...process.env,
      CARGO_TARGET_DIR: join(common.stdout.trim(), "gram-code-build", "monty"),
    },
  })`mise run build:monty`;
  await $({ stdio: "inherit" })`mise run build:code-runner`;
}
