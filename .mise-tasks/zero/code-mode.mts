#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Configure the native local Code Mode runtime"
//MISE dir="{{ config_root }}"

import { randomBytes } from "node:crypto";
import { existsSync, readFileSync, writeFileSync, chmodSync } from "node:fs";
import { join } from "node:path";
import { parseTOML } from "confbox";

type LocalConfig = { env?: Record<string, unknown> };

const filename = join(process.cwd(), "mise.local.toml");
let source = existsSync(filename) ? readFileSync(filename, "utf8") : "";
const config = parseTOML<LocalConfig>(source);
const additions: string[] = [];
const replacements: [key: string, value: string][] = [];

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function ensureSetting(key: string, fallback: string): string {
  const saved = config.env?.[key];
  if (typeof saved === "string" && saved && saved !== "unset") return saved;
  // An existing non-string or empty entry needs an intentional operator edit;
  // the "unset" sentinel means missing and is replaced below.
  if (saved !== undefined && saved !== "unset") {
    throw new Error(`Set ${key} to a non-empty string in mise.local.toml.`);
  }
  const inherited = process.env[key];
  const value = inherited && inherited !== "unset" ? inherited : fallback;
  if (saved === "unset") replacements.push([key, value]);
  else additions.push(`${key} = ${JSON.stringify(value)}`);
  return value;
}

const provider = ensureSetting("GRAM_CODE_RUNTIME_PROVIDER", "local");
if (provider === "local") {
  ensureSetting("GRAM_CODE_RUNTIME_TOKEN", randomBytes(32).toString("hex"));
}
for (const [key, value] of replacements) {
  const sentinel = new RegExp(
    `^([ \\t]*["']?${escapeRegExp(key)}["']?[ \\t]*=[ \\t]*)(?:"unset"|'unset')`,
    "m",
  );
  if (!sentinel.test(source)) {
    throw new Error(`Replace ${key} = "unset" in mise.local.toml by hand.`);
  }
  source = source.replace(
    sentinel,
    (_, prefix: string) => prefix + JSON.stringify(value),
  );
}
if (additions.length || replacements.length) {
  if (!source.endsWith("\n")) source += "\n";
  const header = /^\[env\][ \t]*(?:#[^\n]*)?\r?\n/m;
  if (additions.length && header.test(source)) {
    source = source.replace(
      header,
      (match) => match + additions.join("\n") + "\n",
    );
  } else if (additions.length && config.env === undefined) {
    source += "\n[env]\n" + additions.join("\n") + "\n";
  } else if (additions.length) {
    throw new Error("Local configuration needs an [env] table.");
  }
  // Validate before writing; keep credentials out of command arguments and logs.
  const written = parseTOML<LocalConfig>(source);
  for (const [key, value] of replacements) {
    if (written.env?.[key] !== value) {
      throw new Error(`Replace ${key} = "unset" in mise.local.toml by hand.`);
    }
  }
  writeFileSync(filename, source, { mode: 0o600 });
}
// The file holds the runner token, so keep it private on every run, including
// when an earlier run already wrote every setting.
if (
  existsSync(filename) &&
  parseTOML<LocalConfig>(source).env?.["GRAM_CODE_RUNTIME_TOKEN"] !== undefined
) {
  chmodSync(filename, 0o600);
}
// The runtime itself is the speakeasy-api/codemode release pinned in mise.toml;
// mise install downloads it, so there is nothing to build here.
console.log("Code Mode settings ready in mise.local.toml.");
