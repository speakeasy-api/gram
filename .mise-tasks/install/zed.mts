#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Set up Zed editor settings"

import {
  readFileSync,
  existsSync,
  copyFileSync,
  writeFileSync,
  mkdirSync,
} from "node:fs";
import { join } from "node:path";

const unchanged = Symbol.for("unchanged");

if (!process.env["_"]) {
  console.error(
    ":rotating_light: Special mise environment variable '_' is not set.",
  );
  console.error(
    ":rotating_light: This script is meant to run within a mise environment.",
  );
  process.exit(1);
}

if (!existsSync(".zed")) {
  mkdirSync(".zed");
}

function amendDebugConfigs(
  debugConfigsJSON: string,
): string | typeof unchanged {
  const edited = JSON.parse(debugConfigsJSON);

  if (!Array.isArray(edited)) {
    console.error(
      ":rotating_light: Invalid debug configurations format. Expected it to be an array.",
    );
    return unchanged;
  }

  if (
    edited.find(
      (cfg: unknown) =>
        typeof cfg === "object" &&
        cfg != null &&
        "label" in cfg &&
        cfg.label === "Server",
    )
  ) {
    return unchanged;
  }

  edited.unshift({
    label: "Server",
    adapter: "Delve",
    request: "launch",
    program: "$ZED_WORKTREE_ROOT/server",
    args: ["start"],
    cwd: "$ZED_WORKTREE_ROOT",
    env: {
      TEMPORAL_DEBUG: "true",
      GRAM_SINGLE_PROCESS: "true",
    },
  });

  return JSON.stringify(edited, null, 2);
}

function removeCommentLines(jsonString: string): string {
  return jsonString
    .split("\n")
    .filter(
      (line) => !line.trim().startsWith("//") && !line.trim().startsWith("#"),
    )
    .join("\n");
}

function writeDebugConfigs() {
  const found = existsSync(".zed/debug.json");

  try {
    const debugConfigs = found
      ? readFileSync(".zed/debug.json", "utf-8")
      : "[]";

    const edited = amendDebugConfigs(removeCommentLines(debugConfigs));
    if (edited === unchanged) {
      return;
    }

    if (found) {
      copyFileSync(".zed/debug.json", ".zed/debug.json.old");
    }

    writeFileSync(".zed/debug.json", edited);
  } catch (e) {
    console.error(":rotating_light: Could not update .zed/debug.json");
    console.error(e);
  }
}

writeDebugConfigs();
