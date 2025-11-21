#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Setup Gram Functions to use Fly.io during development."
//MISE dir="{{ config_root }}"
//USAGE flag "--restart" default="false" help="Force the onboarding even if configuration already exists."

import process from "node:process";
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
  log,
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
    - A Fly.io account (https://fly.io)
    - A Fly.io organization-scoped token (https://fly.io/tokens/create)
    - A Fly.io app hosting the the Gram Functions runner images
`.slice(1, -1),
    "Pre-requisites",
  );
  const proceed = await confirm({ message: "Are you ready to proceed?" });
  if (isCancel(proceed)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }
  if (!proceed) {
    const args = ["GRAM_FUNCTIONS_PROVIDER=local"];
    await $`mise set --file mise.local.toml ${args}`;

    outro(
      "Defaulted to stubbed local provider. To start this onboarding again, run `mise run zero:fly --restart`.",
    );
    process.exit(0);
  }

  const token = await password({
    message: "💬 Enter your Fly.io organization-scoped token",
    validate: (value) => {
      if (!value?.startsWith("FlyV1 ")) {
        return "Invalid Fly.io token. It should start with 'FlyV1 ...'.";
      }
    },
  });
  if (isCancel(token)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }

  const org = await text({
    message: "💬 Enter your Fly.io organization name",
  });
  if (isCancel(org)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }

  const app = await text({
    message: "💬 Enter your Fly.io app name for Gram Functions runner images",
  });
  if (isCancel(app)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }

  const args = [
    `GRAM_FUNCTIONS_PROVIDER=flyio`,
    `GRAM_FUNCTIONS_FLYIO_ORG=${org}`,
    `GRAM_FUNCTIONS_FLYIO_API_TOKEN=${token}`,
    `GRAM_FUNCTIONS_RUNNER_OCI_IMAGE=registry.fly.io/${app}`,
    `GRAM_FUNCTIONS_RUNNER_VERSION=main`,
    `GRAM_FUNCTIONS_FLYIO_REGION=us`,
  ];

  await $`mise set --file mise.local.toml ${args}`;

  outro(
    `✅ Updated mise.local.toml. You're ready to deploy Gram Functions to Fly.io!`,
  );
}

await run();
