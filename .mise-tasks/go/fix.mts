#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Run go fix on the specified files"
//MISE dir="{{ config_root }}"

//USAGE arg "[files]..." help="Go files to fix; read from stdin, one per line, when omitted"

import { existsSync } from "node:fs";
import path from "node:path";
import { $ } from "zx";

// hk hands the file list over stdin rather than argv: the repo-wide list is
// large enough to exceed Linux's per-argument size cap once hk renders it into
// a single shell command. Arguments still work for direct invocation.
async function inputFiles(): Promise<string[]> {
  const args = process.argv.slice(2);
  if (args.length > 0) {
    return args;
  }

  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) {
    chunks.push(chunk as Buffer);
  }

  return Buffer.concat(chunks)
    .toString("utf8")
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line !== "");
}

// Returns the directory of the nearest go.mod at or above dir. The repository
// holds more than one Go module (the root module and glint), and `go fix`
// only accepts packages from the module it runs in.
function moduleRoot(dir: string): string {
  let current = dir;
  while (!existsSync(path.join(current, "go.mod"))) {
    const parent = path.dirname(current);
    if (parent === current) {
      throw new Error(`no go.mod found above ${dir}`);
    }
    current = parent;
  }
  return current;
}

async function run() {
  const files = await inputFiles();
  if (files.length === 0) {
    return;
  }

  const dirsByModule = new Map<string, Set<string>>();
  for (const f of files) {
    const dir = path.dirname(path.resolve(f));
    const root = moduleRoot(dir);
    const relpath = path.relative(root, dir);
    const pkg = relpath === "" ? "." : `./${relpath}`;
    (
      dirsByModule.get(root) ?? dirsByModule.set(root, new Set()).get(root)!
    ).add(pkg);
  }

  for (const [root, pkgs] of dirsByModule) {
    // exhaustruct v5 cannot analyze Go 1.27's direct embedded-field literals.
    $.sync`go -C ${root} fix -embedlit=false ${[...pkgs]}`;
  }
}

await run();
