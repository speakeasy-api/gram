#!/usr/bin/env -S node

//MISE description="Install Claude Code hooks for auto-formatting"
//MISE hide=true
//MISE dir="{{ config_root }}"

import path from "node:path";
import { existsSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { confirm, isCancel } from "@clack/prompts";

const settingsPath = path.join(".claude", "settings.local.json");
const markerPath = path.join(".claude", ".gram-install-prompted");

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

  if (!existsSync(".claude")) {
    mkdirSync(".claude");
  }

  const yes = await confirm({
    message:
      "Do you want to enable auto-formatting hooks for Claude Code? (runs hk fix after edits)",
  });

  if (!isCancel(yes) && yes) {
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
          command: "jq -r '.tool_input.file_path' | xargs hk fix --no-stage",
        },
      ],
    });

    writeFileSync(settingsPath, JSON.stringify(settings, null, 2) + "\n");
  }

  writeFileSync(markerPath, "");
}

await run();
