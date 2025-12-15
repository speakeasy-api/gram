#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Set up VS Code / Cursor editor settings"

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
  console.error("🚨 Special mise environment variable '_' is not set.");
  console.error("🚨 This script is meant to run within a mise environment.");
  process.exit(1);
}

if (!process.env["HOME"]) {
  console.error("🚨 Environment variable 'HOME' is not set.");
  process.exit(1);
}

const miseDataDir = join(process.env["HOME"], ".local", "share", "mise");
const linterPath = join(miseDataDir, "shims", "golangci-lint");
if (!existsSync(linterPath)) {
  console.error("🚨 golangci-lint binary not found at ", linterPath);
  console.error("🚨 Did you run `mise install`?");
  process.exit(1);
}

if (!existsSync(".vscode")) {
  mkdirSync(".vscode");
}

function amendSettings(settingsJSON: string): string {
  const edited = JSON.parse(settingsJSON);

  edited["typescript.tsdk"] = "node_modules/typescript/lib";
  edited["go.lintTool"] = "golangci-lint-v2";
  edited["go.lintFlags"] = [
    "run",
    "--fast-only",
    "--output.text.path=stdout",
    "--show-stats=false",
    "--output.text.print-issued-lines=false",
    "--output.text.colors=true",
  ];

  edited["go.alternateTools"] ??= {};

  edited["go.alternateTools"]["golangci-lint-v2"] = linterPath;

  edited["workbench.editorAssociations"] ??= {};
  edited["workbench.editorAssociations"]["*.md"] ??= "default";

  [
    "[javascript]",
    "[javascriptreact]",
    "[typescript]",
    "[typescriptreact]",
    "[json]",
    "[jsonc]",
    "[css]",
    "[mdx]",
  ].forEach((lang) => {
    edited[lang] ??= {};
    edited[lang]["editor.defaultFormatter"] ??= "esbenp.prettier-vscode";
  });

  return JSON.stringify(edited, null, 2);
}

function amendLaunchConfigs(
  launchConfigsJSON: string,
): string | typeof unchanged {
  const edited = JSON.parse(launchConfigsJSON);

  edited["configurations"] ??= [];
  if (!Array.isArray(edited["configurations"])) {
    console.error(
      "🚨 Invalid launch configurations format. Expected it to be an array.",
    );
    return unchanged;
  }

  if (
    edited.configurations.find(
      (cfg: unknown) =>
        typeof cfg === "object" &&
        cfg != null &&
        "name" in cfg &&
        cfg.name === "Server",
    )
  ) {
    return unchanged;
  }

  edited.configurations.unshift({
    name: "Server",
    type: "go",
    request: "launch",
    mode: "auto",
    program: "${workspaceFolder}/server",
    args: ["start"],
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

function writeSettings() {
  const found = existsSync(".vscode/settings.json");
  if (found) {
    copyFileSync(".vscode/settings.json", ".vscode/settings.json.old");
  }

  try {
    const settings = found
      ? readFileSync(".vscode/settings.json", "utf-8")
      : "{}";

    const edited = amendSettings(removeCommentLines(settings));
    writeFileSync(".vscode/settings.json", edited);
  } catch (e) {
    console.error("🚨 Could not update .vscode/settings.json");
    console.error(e);
  }
}

function writeLaunchConfigs() {
  const found = existsSync(".vscode/launch.json");

  try {
    const launchConfigs = found
      ? readFileSync(".vscode/launch.json", "utf-8")
      : JSON.stringify({ version: "0.2.0", configurations: [] });

    const edited = amendLaunchConfigs(removeCommentLines(launchConfigs));
    if (edited === unchanged) {
      return;
    }

    if (found) {
      copyFileSync(".vscode/launch.json", ".vscode/launch.json.old");
    }

    writeFileSync(".vscode/launch.json", edited);
  } catch (e) {
    console.error("🚨 Could not update .vscode/launch.json");
    console.error(e);
  }
}

writeSettings();
writeLaunchConfigs();
