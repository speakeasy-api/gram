#!/usr/bin/env -S node

//MISE description="Offer to enable the dev-idp (gram dev-idp) for local development"
//MISE hide=true
//USAGE flag "--restart" default="false" help="Force the prompt even if previously configured or skipped."

import { $, question } from "zx";

async function run() {
  const restart = process.env["usage_restart"] === "true";

  if (process.env["GRAM_DEVIDP_SKIPPED"] === "true" && !restart) {
    console.log(
      "⏭️  dev-idp setup previously skipped. Run with `mise run zero:devidp --restart` to reconfigure.",
    );
    process.exit(0);
  }

  const existing = process.env["GRAM_DEVIDP_DATABASE_URL"];
  if (existing && !restart) {
    console.log("✅ GRAM_DEVIDP_DATABASE_URL is already set.");
    process.exit(0);
  }

  console.log(
    "💬 The dev-idp (`gram dev-idp`) is the milestone #0b replacement for mock-speakeasy-idp.",
  );
  console.log(
    "💬 It uses its own Postgres logical database (`gram_devidp`) inside the local gram-db container.",
  );

  const answer = (await question("💬 Enable it locally now? [y/N] "))
    .trim()
    .toLowerCase();

  if (answer !== "y" && answer !== "yes") {
    await $`touch mise.local.toml`;
    await $`mise set --file mise.local.toml GRAM_DEVIDP_SKIPPED=true`;
    console.log(
      "⏭️  Skipping dev-idp setup. Re-run with `mise run zero:devidp --restart` later.",
    );
    process.exit(0);
  }

  const dbUser = process.env["DB_USER"] || "gram";
  const dbPassword = process.env["DB_PASSWORD"] || "gram";
  const dbHost = process.env["DB_HOST"] || "127.0.0.1";
  const dbPort = process.env["DB_PORT"] || "5439";
  const url = `postgres://${dbUser}:${dbPassword}@${dbHost}:${dbPort}/gram_devidp?sslmode=disable&search_path=public`;

  await $`touch mise.local.toml`;
  await $`mise unset --file mise.local.toml GRAM_DEVIDP_SKIPPED`;
  await $`mise set --file mise.local.toml GRAM_DEVIDP_DATABASE_URL=${url}`;
  console.log("🔑 GRAM_DEVIDP_DATABASE_URL saved to mise.local.toml.");
  console.log(
    "ℹ️  The schema will be applied by the next step in `./zero`. Re-run `mise run db:devidp:apply` any time to re-apply.",
  );
}

run();
