#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Install hk hooks for git or Claude Code"

//USAGE flag "--target <target>" help="Where to install hk hooks" {
//USAGE   choices "git" "claude-code"
//USAGE }

import { readFileSync, writeFileSync, existsSync, mkdirSync } from "node:fs";
import { $ } from "zx";

const target = process.env["usage_target"];

if (target === "git") {
  await $`hk install --mise`;
} else if (target === "claude-code") {
  const settingsPath = ".claude/settings.local.json";

  if (!existsSync(".claude")) {
    mkdirSync(".claude");
  }

  const settings = existsSync(settingsPath)
    ? JSON.parse(readFileSync(settingsPath, "utf-8"))
    : {};

  settings.hooks ??= {};
  settings.hooks.PostToolUse ??= [];

  const hasHook = settings.hooks.PostToolUse.some(
    (entry: { matcher?: string; hooks?: { command?: string }[] }) =>
      entry.matcher === "Edit|Write" &&
      entry.hooks?.some((h) => h.command?.includes("hk fix")),
  );

  if (hasHook) {
    console.log("hk hook already configured in", settingsPath);
    process.exit(0);
  }

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
  console.log("Added hk post-edit hook to", settingsPath);
}
