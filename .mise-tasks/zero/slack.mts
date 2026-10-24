#!/usr/bin/env node

//MISE description="Connect your own Slack test app, so tool approval requests reach you as Slack DMs"

import { $, question } from "zx";

const manifest = "local/slack/app-manifest.yaml";

const keys = {
  botToken: "GRAM_DEV_SLACK_BOT_TOKEN",
  signingSecret: "SLACK_SIGNING_SECRET",
  email: "GRAM_DEV_SLACK_EMAIL",
} as const;

function isSet(name: string): boolean {
  const value = process.env[name];
  return typeof value === "string" && value !== "" && value !== "unset";
}

async function run() {
  if (isSet(keys.botToken) && isSet(keys.signingSecret)) {
    console.log(
      `✅ ${keys.botToken} and ${keys.signingSecret} are already set. Run \`mise run seed\` to connect the workspace again.`,
    );
    process.exit(0);
  }

  console.log(
    "💬 Tool approvals can send each approver a Slack DM with Approve and Deny buttons.",
  );
  console.log(
    "💬 To test that locally you need your own Slack app in a test workspace:",
  );
  console.log(`\t1. Create an app from the manifest in ${manifest}`);
  console.log(
    '\t   (https://api.slack.com/apps?new_app=1, "From a manifest").',
  );
  console.log("\t2. Install it to the workspace.");
  console.log(
    "\t3. Copy the Bot User OAuth Token (OAuth & Permissions) and the Signing Secret (Basic Information).",
  );

  const botToken = (
    await question("💬 Paste the bot token (xoxb-...) or press enter to skip: ")
  ).trim();
  if (!botToken) {
    console.log(
      "⚠️ Skipped. Approvers decide in the dashboard until you run `mise run zero:slack`.",
    );
    process.exit(0);
  }
  if (!botToken.startsWith("xoxb-")) {
    console.log("❌ That is not a bot token. Bot tokens start with xoxb-.");
    process.exit(1);
  }
  const signingSecret = (
    await question("💬 Paste the signing secret: ")
  ).trim();
  if (!signingSecret) {
    console.log("❌ The signing secret is required to verify button clicks.");
    process.exit(1);
  }
  const email = (
    await question(
      "💬 Your Slack account email, if it differs from your git email (enter to skip): ",
    )
  ).trim();

  await $`touch mise.local.toml`;
  await $`mise set --file mise.local.toml ${keys.botToken}=${botToken}`.quiet();
  await $`mise set --file mise.local.toml ${keys.signingSecret}=${signingSecret}`.quiet();
  if (email) {
    await $`mise set --file mise.local.toml ${keys.email}=${email}`.quiet();
  }
  console.log("🔑 Saved to mise.local.toml, which git ignores.");
  console.log("💬 Next:");
  console.log(
    "\t• `mise run seed` connects the local org to the workspace and maps you to your Slack account.",
  );
  console.log("\t• `pitchfork restart server` loads the signing secret.");
  console.log(
    `\t• Tunnel ${process.env["GRAM_SERVER_URL"] ?? "your local server"} to a public HTTPS URL and set the app's Interactivity Request URL to https://<tunnel-host>/slack/interactions.`,
  );
}

run();
