#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Setup database migrations during development."
//MISE dir="{{ config_root }}"
//USAGE flag "--restart" default="false" help="Force the onboarding even if configuration already exists."

import process from "node:process";
import { $ } from "zx";
import { cancel, confirm, isCancel } from "@clack/prompts";

async function main() {
  await setupClickHouseMigrations();
}

async function setupClickHouseMigrations() {
  if (
    !!process.env["CLICKHOUSE_MIGRATION_ENGINE"] &&
    process.env["usage_restart"] !== "true"
  ) {
    return;
  }

  const cmdcheck = await $({
    nothrow: true,
    quiet: true,
  })`command -v atlas`;
  if (cmdcheck.exitCode !== 0) {
    return setClickHouseStrategy("golang-migrate");
  }

  const whoami = await $({
    nothrow: true,
    quiet: true,
  })`atlas whoami`;
  if (whoami.exitCode === 0) {
    return setClickHouseStrategy("atlas");
  }

  const tryLogin = await confirm({
    message:
      "You must have an Atlas Pro account and logged in with the atlas CLI to use it for ClickHouse migrations. Log in now?",
  });
  if (isCancel(tryLogin)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }

  if (!tryLogin) {
    return setClickHouseStrategy("golang-migrate");
  }

  await $`atlas login`;
  return setClickHouseStrategy("atlas");
}

async function setClickHouseStrategy(s: "atlas" | "golang-migrate") {
  console.log("Setting ClickHouse migration engine to", s);
  await $`mise set --file mise.local.toml CLICKHOUSE_MIGRATION_ENGINE=${s}`;
}

main();