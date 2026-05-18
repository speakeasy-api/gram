#!/usr/bin/env -S node

//MISE description="Configure IDP mode: mock-workos (default) or real WorkOS"
//MISE hide=true
//USAGE flag "--restart" default="false" help="Force the onboarding even if configuration already exists."

import { $, question } from "zx";

async function run() {
  const mode = process.env["GRAM_IDP_MODE"] || "mock-workos";

  if (mode === "workos" && process.env["usage_restart"] !== "true") {
    console.log("✅ IDP mode: workos (already configured).");
    process.exit(0);
  }

  if (mode === "mock-workos" && process.env["usage_restart"] !== "true") {
    // Check if the user previously made an explicit choice (not just the default).
    const apiKey = process.env["WORKOS_API_KEY"];
    const hasExplicitChoice =
      (typeof apiKey === "string" && apiKey !== "" && apiKey !== "unset") ||
      process.env["GRAM_IDP_SKIPPED"] === "true";
    if (hasExplicitChoice) {
      console.log("✅ IDP mode: mock-workos (already configured).");
      process.exit(0);
    }
  }

  console.log();
  console.log("💬 Which IDP mode do you want to use?");
  console.log();
  console.log("  1) mock-workos  (default)");
  console.log(
    "     \x1b[90mFully local, zero config. dev-idp emulates the WorkOS API.\x1b[0m",
  );
  console.log(
    "     \x1b[90mUses a hardcoded test user — no external account needed.\x1b[0m",
  );
  console.log();
  console.log("  2) workos");
  console.log(
    "     \x1b[90mReal WorkOS AuthKit login via dev-idp proxy.\x1b[0m",
  );
  console.log("     \x1b[90mRequires a WorkOS API key and client ID.\x1b[0m");
  console.log();

  const choice = await question("💬 Enter 1 or 2 (default: 1): ");

  if (choice.trim() === "2") {
    await setupRealWorkOS();
  } else {
    await $`mise set --file mise.local.toml GRAM_IDP_MODE=mock-workos`;
    await $`mise set --file mise.local.toml GRAM_IDP_SKIPPED=true`;
    console.log();
    console.log("✅ IDP mode: mock-workos. No additional config needed.");
  }
}

async function setupRealWorkOS() {
  console.log();
  const key = await question("💬 WorkOS API Key (sk_test_...): ");
  if (!key.trim()) {
    console.log("❌ API key is required for real WorkOS mode.");
    process.exit(1);
  }

  const clientId = await question("💬 WorkOS Client ID (client_...): ");
  if (!clientId.trim()) {
    console.log("❌ Client ID is required for real WorkOS mode.");
    process.exit(1);
  }

  const devidpURL =
    process.env["GRAM_DEVIDP_EXTERNAL_URL"] || "http://localhost:35291";

  await $`touch mise.local.toml`;
  await $`mise set --file mise.local.toml GRAM_IDP_MODE=workos`;
  await $`mise set --file mise.local.toml WORKOS_API_KEY=${key.trim()}`;
  await $`mise set --file mise.local.toml WORKOS_API_URL=${devidpURL}/workos`;
  await $`mise set --file mise.local.toml GRAM_IDP_CLIENT_ID=${clientId.trim()}`;

  console.log();
  console.log(
    "✅ IDP mode: real WorkOS. Credentials saved to mise.local.toml.",
  );
  console.log("   Restart madprocs to apply.");
}

run();
