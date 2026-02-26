#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Install Claude Code hooks for auto-formatting"
//MISE hide=true
//MISE dir="{{ config_root }}"

import { existsSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { confirm, isCancel } from "@clack/prompts";

const settingsPath = ".claude/settings.local.json";
const markerPath = ".claude/.gram-install-prompted";

function hookExists(): boolean {
  if (!existsSync(settingsPath)) return false;

  const settings = JSON.parse(readFileSync(settingsPath, "utf-8"));
  return (settings.hooks?.PostToolUse ?? []).some(
    (entry: { matcher?: string; hooks?: { command?: string }[] }) =>
      entry.matcher === "Edit|Write" &&
      entry.hooks?.some((h) => h.command?.includes("hk fix")),
  );
}

async function run() {
  if (hookExists() || existsSync(markerPath)) {
    return;
  }

  const yes = await confirm({
    message:
      "Do you want to enable auto-formatting hooks for Claude Code? (runs hk fix after edits)",
  });

  if (!isCancel(yes) && yes) {
    if (!existsSync(".claude")) {
      mkdirSync(".claude");
    }

    const settings = existsSync(settingsPath)
      ? JSON.parse(readFileSync(settingsPath, "utf-8"))
      : {};

    settings.hooks ??= {};
    settings.hooks.PostToolUse ??= [];
    settings.hooks.PostToolUse.push({
      matcher: "Edit|Write",
      hooks: [
        {
          type: "command",
          command: "hk fix --all",
        },
      ],
    });

    writeFileSync(settingsPath, JSON.stringify(settings, null, 2) + "\n");
  }

  writeFileSync(markerPath, "");
}

await run();
