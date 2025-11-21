#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Setup Gram Functions to use Fly.io during development."
//MISE dir="{{ config_root }}"

import process from "node:process";
import { $ } from "zx";
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

async function run() {
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
  if (isCancel(proceed) || !proceed) {
    cancel("Operation cancelled.");
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
