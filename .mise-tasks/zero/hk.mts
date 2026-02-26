#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Install hk git pre-commit hooks"
//MISE hide=true
//MISE dir="{{ config_root }}"

import { existsSync, mkdirSync, writeFileSync, chmodSync } from "node:fs";
import { $ } from "zx";
import { confirm, isCancel } from "@clack/prompts";

async function run() {
  if (existsSync(".git/hooks/pre-commit")) {
    return;
  }

  const yes = await confirm({
    message: "Do you want to install git pre-commit hooks?",
  });

  if (isCancel(yes) || !yes) {
    mkdirSync(".git/hooks", { recursive: true });
    writeFileSync(".git/hooks/pre-commit", "#!/bin/sh\n");
    chmodSync(".git/hooks/pre-commit", 0o755);
    return;
  }

  await $`hk install --mise`;
}

await run();
