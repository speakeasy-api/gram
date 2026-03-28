#!/usr/bin/env -S node

//MISE description="Setup Gram Functions to use Fly.io during development."
//MISE dir="{{ config_root }}"
//USAGE flag "--restart" default="false" help="Force the onboarding even if configuration already exists."

import process from "node:process";
import os from "node:os";
import { $ } from "zx";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import {
  intro,
  note,
  outro,
  confirm,
  isCancel,
  cancel,
  text,
  password,
} from "@clack/prompts";

function checkExistingConfig(): { exists: boolean; hasProvider: boolean } {
  const configPath = join(process.cwd(), "mise.local.toml");

  if (!existsSync(configPath)) {
    return { exists: false, hasProvider: false };
  }

  const content = readFileSync(configPath, "utf-8");
  const hasProvider = /^\s*GRAM_FUNCTIONS_PROVIDER\s*=/gm.test(content);

  return { exists: true, hasProvider };
}

async function fallbackToLocal() {
  const args = ["GRAM_FUNCTIONS_PROVIDER=local"];
  await $`mise set --file mise.local.toml ${args}`;
  outro(
    "Defaulted to stubbed local provider. To start this onboarding again, run `mise run zero:fly --restart`.",
  );
  process.exit(0);
}

function randomAppName() {
  const chars = "abcdefghijklmnopqrstuvwxyz0123456789";
  const bytes = crypto.getRandomValues(new Uint8Array(6));
  const suffix = Array.from(bytes, (b) => chars[b % chars.length]).join("");

  let username = "";

  try {
    username = os.userInfo().username;
  } catch {
    // Ignore error and set up fallback username later
  }

  const user = username.toLowerCase().replaceAll(".", "-") || "user";
  return `${user}-${suffix}`;
}

async function run() {
  if (
    checkExistingConfig().hasProvider &&
    process.env["usage_restart"] !== "true"
  ) {
    console.log(
      "GRAM_FUNCTIONS_PROVIDER already configured in mise.local.toml. To start fly.io onboarding again, run `mise run zero:fly --restart`.",
    );
    process.exit(0);
  }

  intro(`Gram Functions Fly.io Setup 🛫`);

  note(
    `
👀 To deploy Gram Functions to Fly.io, you'll need:
    🎈 A Fly.io account (https://fly.io)
    🎈 A Fly.io organization-scoped token (https://fly.io/tokens/create or \`fly tokens create org --name <name>\`)
    🎈 A Fly.io app hosting the the Gram Functions runner images
    🐅 A Tigris bucket associated with the Fly.io organization
    🐅 A Tigris Access Key ID and Secret Access Key with permissions to access the bucket (https://console.tigris.dev)
`.slice(1, -1),
    "Pre-requisites",
  );
  const proceed = await confirm({
    message: "Set up Fly.io and Tigris",
    active: "Start",
    inactive: "Skip",
  });
  if (isCancel(proceed)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }
  if (!proceed) {
    await fallbackToLocal();
  }

  const initialToken =
    process.env["GRAM_FUNCTIONS_FLYIO_API_TOKEN"] || undefined;
  let tokenMessage = "🎈 Enter your Fly.io organization-scoped token";
  if (initialToken) {
    tokenMessage += " (leave blank to keep existing)";
  }
  let token = await password({
    message: tokenMessage,
    validate: (value) => {
      if (!value) return;
      if (!value.startsWith("FlyV1 ")) {
        return "Invalid Fly.io token. It should start with 'FlyV1 ...'. Leave blank to skip.";
      }
    },
  });
  if (isCancel(token)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }
  if (!token && initialToken) {
    token = initialToken;
  }
  if (!token) {
    await fallbackToLocal();
  }

  const initialOrg = process.env["GRAM_FUNCTIONS_FLYIO_ORG"] || undefined;
  const org = await text({
    message: "🎈 Enter your Fly.io organization name",
    initialValue: initialOrg,
  });
  if (isCancel(org)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }

  const initialApp =
    process.env["GRAM_FUNCTIONS_RUNNER_OCI_IMAGE"]?.split("/")[1] ||
    randomAppName();
  const app = await text({
    message:
      "🎈 Enter your Fly.io app name for Gram Functions runner images (accept the default if unsure)",
    initialValue: initialApp,
  });
  if (isCancel(app)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }

  const initialTigrisBucket =
    process.env["GRAM_FUNCTIONS_TIGRIS_BUCKET_URI"]?.slice("s3://".length) ||
    undefined;
  const bucket = await text({
    message: "🐅 Enter your Tigris bucket name for Gram Functions",
    initialValue: initialTigrisBucket,
  });
  if (isCancel(bucket)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }

  const initialTigrisKey =
    process.env["GRAM_FUNCTIONS_TIGRIS_KEY"] || undefined;
  const tigrisKey = await text({
    message: `🐅 Enter your Tigris Access Key ID for ${bucket} (leave blank to skip)`,
    initialValue: initialTigrisKey,
    validate: (value) => {
      if (!value) return;
      if (!value.startsWith("tid_")) {
        return "Invalid Tigris Access Key ID. It should start with 'tid_'. Leave blank to skip.";
      }
    },
  });
  if (isCancel(tigrisKey)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }
  if (!tigrisKey) {
    await fallbackToLocal();
  }

  const initialTigrisSecret =
    process.env["GRAM_FUNCTIONS_TIGRIS_SECRET"] || undefined;
  let tigrisSecretMessage = `🐅 Enter your Tigris Secret Access Key for ${bucket}`;
  if (initialTigrisSecret) {
    tigrisSecretMessage += " (leave blank to keep existing)";
  }
  let tigrisSecret = await password({
    message: tigrisSecretMessage,
    validate: (value) => {
      if (!value) return;
      if (!value.startsWith("tsec_")) {
        return "Invalid Tigris Secret Access Key. It should start with 'tsec_'. Leave blank to skip.";
      }
    },
  });
  if (isCancel(tigrisSecret)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }
  if (!tigrisSecret && initialTigrisSecret) {
    tigrisSecret = initialTigrisSecret;
  }
  if (!tigrisSecret) {
    await fallbackToLocal();
  }

  const args = [
    `GRAM_FUNCTIONS_PROVIDER=flyio`,
    `GRAM_FUNCTIONS_FLYIO_ORG=${org}`,
    `GRAM_FUNCTIONS_FLYIO_API_TOKEN=${token}`,
    `GRAM_FUNCTIONS_RUNNER_OCI_IMAGE=registry.fly.io/${app}`,
    `GRAM_FUNCTIONS_RUNNER_VERSION=main`,
    `GRAM_FUNCTIONS_FLYIO_REGION=us`,
    `GRAM_FUNCTIONS_TIGRIS_BUCKET_URI=s3://${bucket}`,
    `GRAM_FUNCTIONS_TIGRIS_KEY=${tigrisKey}`,
    `GRAM_FUNCTIONS_TIGRIS_SECRET=${tigrisSecret}`,
  ];

  await $`mise set --file mise.local.toml ${args}`;

  outro(
    `✅ Updated mise.local.toml. You're ready to deploy Gram Functions to Fly.io!`,
  );
}

await run();
