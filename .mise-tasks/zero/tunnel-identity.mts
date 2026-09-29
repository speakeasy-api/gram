#!/usr/bin/env node

//MISE description="Setup the tunnel caller identity signing key for local development."
//MISE hide=true

import { createPublicKey, generateKeyPairSync } from "node:crypto";
import { $ } from "zx";

const PRIVATE_KEY = "GRAM_AUTHZ_PRIVATE_KEY";
const PUBLIC_KEYS = "GRAM_AUTHZ_PUBLIC_KEYS";

function isConfigured(value: string | undefined): value is string {
  return typeof value === "string" && !!value && value !== "unset";
}

async function setKey(key: string, value: string) {
  await $`touch mise.local.toml`;
  await $`mise set --file mise.local.toml ${key}=${value}`;
  console.log(`🔑 ${key} has been set in mise.local.toml`);
}

async function run() {
  let privateKey = process.env[PRIVATE_KEY];
  if (isConfigured(privateKey)) {
    console.log(`✅ ${PRIVATE_KEY} is already set.`);
  } else {
    console.log(`💬 ${PRIVATE_KEY} will be generated`);
    privateKey = generateKeyPairSync("rsa", {
      modulusLength: 3072,
      publicKeyEncoding: { type: "spki", format: "pem" },
      privateKeyEncoding: { type: "pkcs8", format: "pem" },
    }).privateKey;
    await setKey(PRIVATE_KEY, privateKey);
  }

  // The server refuses to start unless the published bundle contains the
  // signing key, so regenerate the bundle whenever the private key is new.
  if (
    isConfigured(process.env[PUBLIC_KEYS]) &&
    isConfigured(process.env[PRIVATE_KEY])
  ) {
    console.log(`✅ ${PUBLIC_KEYS} is already set.`);
    return;
  }
  const publicKey = createPublicKey(privateKey).export({
    type: "spki",
    format: "pem",
  });
  await setKey(PUBLIC_KEYS, publicKey.toString());
}

run();
