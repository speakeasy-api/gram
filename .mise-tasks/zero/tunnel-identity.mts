#!/usr/bin/env node

//MISE description="Setup the tunnel caller identity signing key for local development."
//MISE hide=true

import { execFileSync } from "node:child_process";
import { createPublicKey, generateKeyPairSync } from "node:crypto";

const PRIVATE_KEY = "GRAM_AUTHZ_PRIVATE_KEY";
const PUBLIC_KEYS = "GRAM_AUTHZ_PUBLIC_KEYS";

function isConfigured(value: string | undefined): value is string {
  return typeof value === "string" && value !== "" && value !== "unset";
}

// Secrets go through stdin so they never appear in a process command line.
function setSecret(key: string, value: string): void {
  execFileSync("mise", ["set", "--file", "mise.local.toml", "--stdin", key], {
    input: value,
    stdio: ["pipe", "ignore", "inherit"],
  });
  console.log(`🔑 ${key} has been set in mise.local.toml`);
}

function publicKeyDER(key: string): string {
  return createPublicKey(key)
    .export({ type: "spki", format: "der" })
    .toString("base64");
}

// The server refuses to start unless the bundle contains the signing key.
function bundleContains(bundle: string, privateKey: string): boolean {
  const want = publicKeyDER(privateKey);
  const blocks =
    bundle.match(
      /-----BEGIN PUBLIC KEY-----[\s\S]+?-----END PUBLIC KEY-----/g,
    ) ?? [];
  return blocks.some((block) => publicKeyDER(block) === want);
}

function run(): void {
  let privateKey = process.env[PRIVATE_KEY];
  let publicKeys = process.env[PUBLIC_KEYS];

  if (isConfigured(privateKey)) {
    console.log(`✅ ${PRIVATE_KEY} is already set.`);
  } else {
    console.log(`💬 ${PRIVATE_KEY} will be generated`);
    privateKey = generateKeyPairSync("rsa", {
      modulusLength: 3072,
      publicKeyEncoding: { type: "spki", format: "pem" },
      privateKeyEncoding: { type: "pkcs8", format: "pem" },
    }).privateKey;
    setSecret(PRIVATE_KEY, privateKey);
    publicKeys = undefined;
  }

  if (!isConfigured(publicKeys)) {
    setSecret(
      PUBLIC_KEYS,
      createPublicKey(privateKey)
        .export({ type: "spki", format: "pem" })
        .toString(),
    );
    return;
  }
  if (!bundleContains(publicKeys, privateKey)) {
    console.error(
      `❌ ${PUBLIC_KEYS} does not contain the public key for ${PRIVATE_KEY}. ` +
        `Remove both from mise.local.toml and rerun \`mise run zero:tunnel-identity\`.`,
    );
    process.exit(1);
  }
  console.log(`✅ ${PUBLIC_KEYS} is already set.`);
}

run();
