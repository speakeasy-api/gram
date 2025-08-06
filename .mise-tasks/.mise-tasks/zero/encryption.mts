#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Setup a Gram encryption key for local development."
//MISE hide=true

import { $ } from "zx";
import { randomBytes } from "node:crypto";

async function run() {
  const existing = process.env["GRAM_ENCRYPTION_KEY"];
  if (typeof existing === "string" && !!existing && existing !== "unset") {
    console.log("✅ GRAM_ENCRYPTION_KEY is already set.");
    process.exit(0);
  }

  console.log(
    "💬 Gram encryption key will be generated to encrypt environment variables."
  );

  const secret = randomBytes(32).toString("base64");

  await setKey(secret);
}

async function setKey(value: string) {
  await $`touch mise.local.toml`;
  await $`mise set --file mise.local.toml GRAM_ENCRYPTION_KEY=${value}`;
  console.log("🔑 GRAM_ENCRYPTION_KEY has been set in mise.local.toml");
}

run();
