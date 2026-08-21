#!/usr/bin/env -S node

//MISE description="Configure the local assistant runtime (provider, image, and initial build)"
//MISE hide=true
//MISE dir="{{ config_root }}"
//USAGE flag "--restart" default="false" help="Force local provider/image and rebuild the runtime image."
//USAGE flag "--skip-image" default="false" help="Write env vars only; do not build the runtime image."

import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { $ } from "zx";

const LOCAL_PROVIDER = "local";
const LOCAL_IMAGE = "gram-assistant-runtime";
const PROVIDER_KEY = "GRAM_ASSISTANT_RUNTIME_PROVIDER";
const IMAGE_KEY = "GRAM_ASSISTANT_RUNTIME_OCI_IMAGE";

function isSet(value: string | undefined): boolean {
  return typeof value === "string" && value !== "" && value !== "unset";
}

function localConfigValue(key: string): string | undefined {
  const configPath = join(process.cwd(), "mise.local.toml");
  if (!existsSync(configPath)) {
    return undefined;
  }

  const match = new RegExp(`^\\s*${key}\\s*=\\s*(.+?)\\s*$`, "m").exec(
    readFileSync(configPath, "utf-8"),
  );
  if (!match) {
    return undefined;
  }

  const value = match[1].trim().replace(/^["']|["']$/g, "");
  return isSet(value) ? value : undefined;
}

async function saveConfig(key: string, value: string): Promise<void> {
  await $`touch mise.local.toml`;
  await $`mise set --file mise.local.toml ${key}=${value}`;
  console.log(`🔑 ${key} has been set in mise.local.toml`);
}

async function dockerAvailable(): Promise<boolean> {
  const result = await $`docker info`.nothrow().quiet();
  return result.ok;
}

async function imageExists(image: string): Promise<boolean> {
  const result = await $`docker images -q ${image}`.nothrow().quiet();
  return result.ok && result.stdout.trim() !== "";
}

async function ensureLocalConfig(
  key: string,
  fallback: string,
  restart: boolean,
): Promise<string> {
  const existing = localConfigValue(key);
  if (!restart && existing) {
    console.log(`✅ ${key} is already set.`);
    return existing;
  }

  // Persist a value already in the process environment (intentional override
  // or a leftover from the old mise.toml defaults) so a later clean shell
  // still has it. Fall back to the local default when nothing is set.
  const value = restart
    ? fallback
    : isSet(process.env[key])
      ? process.env[key]!
      : fallback;
  await saveConfig(key, value);
  return value;
}

async function run() {
  const restart = process.env["usage_restart"] === "true";
  const skipImage = process.env["usage_skip_image"] === "true";

  const provider = await ensureLocalConfig(
    PROVIDER_KEY,
    LOCAL_PROVIDER,
    restart,
  );
  const imageName = await ensureLocalConfig(IMAGE_KEY, LOCAL_IMAGE, restart);
  const imageRef = `${imageName}:dev`;

  if (skipImage) {
    return;
  }

  if (provider !== LOCAL_PROVIDER) {
    console.log(
      `✅ Assistant runtime provider is ${provider}; skipping local image build.`,
    );
    return;
  }

  if (!(await dockerAvailable())) {
    console.log(
      "⚠️ Docker is not available; skipping assistant runtime image build.",
    );
    console.log(
      "⚠️ Run `mise run build:assistants-runtime-image` after Docker is up.",
    );
    return;
  }

  if (!restart && (await imageExists(imageRef))) {
    console.log(`✅ Assistant runtime image ${imageRef} already exists.`);
    return;
  }

  console.log(`💬 Building assistant runtime image ${imageRef}...`);
  await $({ stdio: "inherit" })`mise run build:assistants-runtime-image`;
}

await run();
