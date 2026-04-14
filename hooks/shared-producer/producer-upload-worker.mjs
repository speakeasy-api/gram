#!/usr/bin/env node

import { stderr } from "node:process";

import { runUploadWorkerFromFile } from "./upload.mjs";

function parseRequestFileArg(argv = process.argv.slice(2)) {
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--request-file") {
      return argv[i + 1] ?? null;
    }
    if (arg.startsWith("--request-file=")) {
      return arg.slice("--request-file=".length);
    }
  }
  return null;
}

const requestFile = parseRequestFileArg();
if (!requestFile) {
  stderr.write(
    "[gram-skills-producer] missing --request-file for upload worker\n",
  );
  process.exit(1);
}

const result = await runUploadWorkerFromFile(requestFile);
if (!result.ok && !result.skipped) {
  stderr.write(
    `[gram-skills-producer] upload worker failed: ${result.reason ?? "unknown"}${result.status ? ` status=${result.status}` : ""}\n`,
  );
}
