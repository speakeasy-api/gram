#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Install hk git pre-commit hooks"
//MISE hide=true
//MISE dir="{{ config_root }}"

import { existsSync, mkdirSync, writeFileSync, chmodSync } from "node:fs";
import { join } from "node:path";
import { $ } from "zx";
import { confirm, isCancel } from "@clack/prompts";

async function run() {
  const gitDir = (await $`git rev-parse --git-common-dir`).stdout.trim();
  const hooksDir = join(gitDir, "hooks");
  const preCommit = join(hooksDir, "pre-commit");

  if (existsSync(preCommit)) {
    return;
  }

  const yes = await confirm({
    message: "Do you want to install git pre-commit hooks?",
  });

  if (isCancel(yes) || !yes) {
    mkdirSync(hooksDir, { recursive: true });
    writeFileSync(preCommit, "");
    chmodSync(preCommit, 0o755);
    return;
  }

  await $`hk install --mise`;
}

await run();
