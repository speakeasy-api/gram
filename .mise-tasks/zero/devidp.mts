#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Generate dev-idp signing and client credentials for local development."
//MISE hide=true

import { generateKeyPairSync, randomBytes } from "node:crypto";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { $ } from "zx";

const RSA_KEY = "GRAM_DEVIDP_RSA_PRIVATE_KEY";
const CLIENT_SECRET = "GRAM_IDP_CLIENT_SECRET";
const WORKOS_API_KEY = "WORKOS_API_KEY";
const OIDC_CLIENT_SECRET = "OIDC_CLIENT_SECRET";

function isConfigured(value: string | undefined): boolean {
  return typeof value === "string" && value !== "" && value !== "unset";
}

function setSecret(key: string, value: string): void {
  execFileSync("mise", ["set", "--file", "mise.local.toml", "--stdin", key], {
    input: value,
    stdio: ["pipe", "ignore", "inherit"],
  });
}

async function run() {
  await $`touch mise.local.toml`;

  const existingClientSecret = process.env[CLIENT_SECRET];
  if (existingClientSecret?.startsWith("sk_")) {
    if (!isConfigured(process.env[WORKOS_API_KEY])) {
      setSecret(WORKOS_API_KEY, existingClientSecret);
      console.log(`✅ Moved the WorkOS API key to ${WORKOS_API_KEY}.`);
    }
    const clientSecret = `devidp_${randomBytes(32).toString("base64url")}`;
    setSecret(CLIENT_SECRET, clientSecret);
    console.log(`🔑 Replaced ${CLIENT_SECRET} with a dev-idp client secret.`);
  } else if (isConfigured(existingClientSecret)) {
    console.log(`✅ ${CLIENT_SECRET} is already set.`);
  } else {
    console.log(`💬 ${CLIENT_SECRET} will be generated.`);
    const clientSecret = `devidp_${randomBytes(32).toString("base64url")}`;
    setSecret(CLIENT_SECRET, clientSecret);
    console.log(`🔑 ${CLIENT_SECRET} has been set in mise.local.toml`);
  }

  const localConfig = readFileSync("mise.local.toml", "utf8");
  if (new RegExp(`^${OIDC_CLIENT_SECRET}\\s*=`, "m").test(localConfig)) {
    execFileSync(
      "mise",
      ["unset", "--file", "mise.local.toml", OIDC_CLIENT_SECRET],
      {
        stdio: ["ignore", "ignore", "inherit"],
      },
    );
    console.log(`✅ Removed the retired ${OIDC_CLIENT_SECRET}.`);
  }

  if (isConfigured(process.env[RSA_KEY])) {
    console.log(`✅ ${RSA_KEY} is already set.`);
  } else {
    console.log(`💬 ${RSA_KEY} will be generated.`);
    const { privateKey } = generateKeyPairSync("rsa", {
      modulusLength: 2048,
      publicKeyEncoding: { type: "spki", format: "pem" },
      privateKeyEncoding: { type: "pkcs8", format: "pem" },
    });

    await $`mise set --file mise.local.toml ${RSA_KEY}=${privateKey}`;
    console.log(`🔑 ${RSA_KEY} has been set in mise.local.toml`);
  }

  // Temporary: evolve databases for dev-idp schema changes introduced on
  // 2026-09-07. Remove this invocation and the temporary evolution path after
  // 2026-10-15, when local environments can be assumed to have run it.
  await $`mise run db:devidp:evolve`;
}

run();
