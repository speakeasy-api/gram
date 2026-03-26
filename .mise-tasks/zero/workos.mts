#!/usr/bin/env -S node

//MISE description="Setup WorkOS AuthKit OIDC credentials for the mock IDP"
//MISE hide=true

import { $, question } from "zx";

const WORKOS_ISSUER = "https://convenient-daydream-57-development.authkit.app/";

async function run() {
  const issuer = process.env["OIDC_ISSUER"];
  if (typeof issuer === "string" && issuer !== "unset") {
    console.log("✅ WorkOS OIDC credentials are already configured.");
    process.exit(0);
  }

  console.log(
    "💬 WorkOS AuthKit can be configured for authentication in local development.",
  );
  console.log(
    "💬 If you don't have WorkOS access, skip this step and the mock IDP will use a hardcoded test user instead.",
  );

  const clientId = await question(
    "💬 Paste your WorkOS Client ID or press enter to skip: ",
  );
  if (!clientId) {
    console.log("⏭️  Skipping WorkOS setup. Mock IDP will run in mock mode.");
    process.exit(0);
  }

  const port = process.env["MOCK_IDP_PORT"] || "35291";
  const host = process.env["MOCK_IDP_HOST"] || "localhost";
  console.log(
    "💬 Make sure you add the following redirect URI to your WorkOS AuthKit config:",
  );
  console.log(`\thttp://${host}:${port}/v1/speakeasy_provider/oidc/callback`);
  console.log();

  const clientSecret = await question("💬 Paste your WorkOS Client Secret: ");
  if (!clientSecret) {
    console.log("❌ Client Secret is required.");
    process.exit(1);
  }

  await $`touch mise.local.toml`;
  await $`mise set --file mise.local.toml OIDC_ISSUER=${WORKOS_ISSUER}`;
  await $`mise set --file mise.local.toml OIDC_CLIENT_ID=${clientId}`;
  await $`mise set --file mise.local.toml OIDC_CLIENT_SECRET=${clientSecret}`;
  console.log("🔑 WorkOS OIDC credentials have been saved to mise.local.toml");
}

run();
