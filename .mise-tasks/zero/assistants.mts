#!/usr/bin/env -S node

//MISE description="Configure the local assistant runtime (provider, image, and initial build)"
//MISE hide=true
//MISE dir="{{ config_root }}"
//USAGE flag "--restart" default="false" help="Force local provider/image and rebuild the runtime image."
//USAGE flag "--skip-image" default="false" help="Write env vars only; do not build the runtime image."

import { $ } from "zx";

const LOCAL_PROVIDER = "local";
const LOCAL_IMAGE = "gram-assistant-runtime";

function isSet(value: string | undefined): boolean {
  return typeof value === "string" && value !== "" && value !== "unset";
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

async function run() {
  const restart = process.env["usage_restart"] === "true";
  const skipImage = process.env["usage_skip_image"] === "true";
  const providerSet = isSet(process.env["GRAM_ASSISTANT_RUNTIME_PROVIDER"]);
  const imageSet = isSet(process.env["GRAM_ASSISTANT_RUNTIME_OCI_IMAGE"]);

  if (restart || !providerSet) {
    await saveConfig("GRAM_ASSISTANT_RUNTIME_PROVIDER", LOCAL_PROVIDER);
  } else {
    console.log("✅ GRAM_ASSISTANT_RUNTIME_PROVIDER is already set.");
  }

  if (restart || !imageSet) {
    await saveConfig("GRAM_ASSISTANT_RUNTIME_OCI_IMAGE", LOCAL_IMAGE);
  } else {
    console.log("✅ GRAM_ASSISTANT_RUNTIME_OCI_IMAGE is already set.");
  }

  const provider =
    restart || !providerSet
      ? LOCAL_PROVIDER
      : process.env["GRAM_ASSISTANT_RUNTIME_PROVIDER"]!;
  const imageName =
    restart || !imageSet
      ? LOCAL_IMAGE
      : process.env["GRAM_ASSISTANT_RUNTIME_OCI_IMAGE"]!;
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
