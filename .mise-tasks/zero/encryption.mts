#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Setup Gram encryption keys for local development."
//MISE hide=true

import { randomBytes } from "node:crypto";
import { $ } from "zx";

const toPopulate = ["GRAM_ENCRYPTION_KEY", "GRAM_JWT_SIGNING_KEY"];

async function run() {
  for (const key of toPopulate) {
    const existing = process.env[key];
    if (typeof existing === "string" && !!existing && existing !== "unset") {
      console.log(`✅ ${key} is already set.`);
      continue;
    }

    console.log(`💬 ${key} will be generated`);

    const secret = randomBytes(32).toString("base64");

    await setKey(key, secret);
  }
}

async function setKey(key: string, value: string) {
  await $`touch mise.local.toml`;
  await $`mise set --file mise.local.toml ${key}=${value}`;
  console.log(`🔑 ${key} has been set in mise.local.toml`);
}

run();
