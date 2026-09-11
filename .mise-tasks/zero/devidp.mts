#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Generate dev-idp signing and client credentials for local development."
//MISE hide=true
//USAGE flag "--skip-evolve" default="false" help="Skip the temporary SQLite schema evolution."

import { generateKeyPairSync, randomBytes } from "node:crypto";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { parseTOML } from "confbox";
import { $ } from "zx";

const RSA_KEY = "GRAM_DEVIDP_RSA_PRIVATE_KEY";
const CLIENT_SECRET = "GRAM_IDP_CLIENT_SECRET";
const WORKOS_API_KEY = "WORKOS_API_KEY";
const OIDC_CLIENT_SECRET = "OIDC_CLIENT_SECRET";
const RETIRED_MODE = "GRAM_IDP_MODE";
const BACKEND = "GRAM_DEVIDP_BACKEND";
const CLIENT_ID = "GRAM_IDP_CLIENT_ID";

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

  const config = parseTOML(readFileSync("mise.local.toml", "utf8")) as {
    env?: Record<string, unknown>;
  };
  const localEnv = config.env ?? {};
  if (localEnv[RETIRED_MODE] === "workos" && !(BACKEND in localEnv)) {
    await $`mise set --file mise.local.toml ${BACKEND}=workos`;
    console.log(`✅ Migrated the identity backend setting to workos.`);
  }
  if (RETIRED_MODE in localEnv) {
    await $`mise unset --file mise.local.toml ${RETIRED_MODE}`;
    console.log(`✅ Removed the retired ${RETIRED_MODE} setting.`);
  }
  const persistedClientID = localEnv[CLIENT_ID];
  if (
    typeof persistedClientID === "string" &&
    persistedClientID.startsWith("client_")
  ) {
    await $`mise unset --file mise.local.toml ${CLIENT_ID}`;
    console.log(`✅ Removed the stale WorkOS client ID override.`);
  }

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
  if (process.env["usage_skip_evolve"] !== "true") {
    await $`mise run db:devidp:evolve`;
  }
}

run();
