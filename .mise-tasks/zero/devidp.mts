#!/usr/bin/env -S node

//MISE description="Generate the dev-idp RSA signing key for local development."
//MISE hide=true

import { generateKeyPairSync } from "node:crypto";
import { $ } from "zx";

const KEY = "GRAM_DEVIDP_RSA_PRIVATE_KEY";

async function run() {
  const existing = process.env[KEY];
  if (typeof existing === "string" && !!existing && existing !== "unset") {
    console.log(`✅ ${KEY} is already set.`);
    return;
  }

  console.log(`💬 ${KEY} will be generated.`);

  const { privateKey } = generateKeyPairSync("rsa", {
    modulusLength: 2048,
    publicKeyEncoding: { type: "spki", format: "pem" },
    privateKeyEncoding: { type: "pkcs8", format: "pem" },
  });

  await $`touch mise.local.toml`;
  await $`mise set --file mise.local.toml ${KEY}=${privateKey}`;
  console.log(`🔑 ${KEY} has been set in mise.local.toml`);
}

run();
