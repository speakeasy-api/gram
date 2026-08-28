#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Run go fix on the specified files"
//MISE dir="{{ config_root }}"

//USAGE arg "[files]..." help="Go files to fix; read from stdin, one per line, when omitted"

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

async function run() {
  const cwd = process.cwd();
  const files = await inputFiles();
  if (files.length === 0) {
    return;
  }

  let dirs = files.map((f) => {
    const relpath = path.relative(cwd, path.dirname(path.resolve(f)));
    return relpath.startsWith("..") ? relpath : `./${relpath}`;
  });
  dirs = [...new Set(dirs)];

  // exhaustruct v5 cannot analyze Go 1.27's direct embedded-field literals.
  $.sync`go fix -embedlit=false ${dirs}`;
}

await run();
