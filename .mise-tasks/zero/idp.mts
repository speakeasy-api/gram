#!/usr/bin/env -S node

//MISE description="Configure the dev-idp identity backend: local (default) or real WorkOS"
//MISE hide=true
//USAGE flag "--restart" default="false" help="Force the onboarding even if configuration already exists."

import { $, question } from "zx";

async function run() {
  const backend = process.env["GRAM_DEVIDP_BACKEND"] || "local";

  if (backend === "workos" && process.env["usage_restart"] !== "true") {
    console.log("✅ Identity backend: workos (already configured).");
    process.exit(0);
  }

  if (backend === "local" && process.env["usage_restart"] !== "true") {
    // Check if the user previously made an explicit choice (not just the default).
    const secret = process.env["GRAM_IDP_CLIENT_SECRET"];
    const hasExplicitChoice =
      (typeof secret === "string" && secret !== "" && secret !== "unset") ||
      process.env["GRAM_IDP_SKIPPED"] === "true";
    if (hasExplicitChoice) {
      console.log("✅ Identity backend: local (already configured).");
      process.exit(0);
    }
  }

  console.log();
  console.log("💬 Which identity backend do you want to use?");
  console.log();
  console.log("  1) local  (default)");
  console.log(
    "     \x1b[90mFully offline, zero config. dev-idp emulates the WorkOS API.\x1b[0m",
  );
  console.log(
    "     \x1b[90mSigns you in as your git committer identity — no external account.\x1b[0m",
  );
  console.log();
  console.log("  2) workos");
  console.log(
    "     \x1b[90mReads and writes your real WorkOS environment through dev-idp.\x1b[0m",
  );
  console.log(
    "     \x1b[90mStill signs you in without credentials, as the WorkOS user\x1b[0m",
  );
  console.log(
    "     \x1b[90mmatching your git committer email. Requires a WorkOS API key.\x1b[0m",
  );
  console.log();

  const choice = await question("💬 Enter 1 or 2 (default: 1): ");

  if (choice.trim() === "2") {
    await setupRealWorkOS();
  } else {
    await $`mise set --file mise.local.toml GRAM_DEVIDP_BACKEND=local`;
    await $`mise set --file mise.local.toml GRAM_IDP_SKIPPED=true`;
    console.log();
    console.log("✅ Identity backend: local. No additional config needed.");
  }
}

async function setupRealWorkOS() {
  console.log();
  const key = await question("💬 WorkOS API Key (sk_test_...): ");
  if (!key.trim()) {
    console.log("❌ API key is required for the workos backend.");
    process.exit(1);
  }

  await $`touch mise.local.toml`;
  await $`mise set --file mise.local.toml GRAM_DEVIDP_BACKEND=workos`;
  await $`mise set --file mise.local.toml GRAM_IDP_CLIENT_SECRET=${key.trim()}`;

  // Deliberately NOT setting GRAM_IDP_CLIENT_ID. The server routes any
  // client_-prefixed id straight to WorkOS's hosted AuthKit, which is an
  // interactive login — the opposite of what dev-idp is for. Leaving the
  // default keeps the authorize leg on dev-idp, which signs you in
  // non-interactively as the WorkOS user matching your committer email.
  //
  // WORKOS_API_URL is likewise left alone: it points at dev-idp's /workos
  // surface in both backends, and the flag above decides what serves it.

  console.log();
  console.log(
    "✅ Identity backend: real WorkOS. Credentials saved to mise.local.toml.",
  );
  console.log(
    "   You'll be signed in as the WorkOS user matching your git committer email.",
  );
  console.log("   Restart pitchfork to apply.");
}

run();
