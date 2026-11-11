#!/usr/bin/env node

//MISE description="Setup organization encryption for local development."
//MISE hide=true

// Organization encryption is off until GRAM_ORG_ENCRYPTION_CREDENTIAL_ID names
// a platform-tier gcp_iam credential. Locally the server reaches KMS through an
// in-process stand-in and a stub identity, so the credential only has to exist
// and impersonate the configured service account. This task picks both values
// once, saves them in mise.local.toml, and upserts the credential rows, so
// re-running it after a database reset restores them.

import { randomUUID } from "node:crypto";
import { $ } from "zx";

const credentialIdKey = "GRAM_ORG_ENCRYPTION_CREDENTIAL_ID";
const serviceAccountKey = "GRAM_ORG_ENCRYPTION_SERVICE_ACCOUNT";
const defaultServiceAccount =
  "org-encryption@gram-local.iam.gserviceaccount.com";

function configured(key: string): string | undefined {
  const value = process.env[key];
  return value && value !== "unset" ? value : undefined;
}

async function setKey(key: string, value: string) {
  await $`touch mise.local.toml`;
  await $`mise set --file mise.local.toml ${key}=${value}`;
  console.log(`🔑 ${key} has been set in mise.local.toml`);
}

async function run() {
  if ((process.env["GRAM_ENVIRONMENT"] ?? "local") !== "local") {
    console.log("⏭️  Organization encryption setup only runs locally.");
    return;
  }

  let credentialId = configured(credentialIdKey);
  if (!credentialId) {
    credentialId = randomUUID();
    await setKey(credentialIdKey, credentialId);
  }
  let serviceAccount = configured(serviceAccountKey);
  if (!serviceAccount) {
    serviceAccount = defaultServiceAccount;
    await setKey(serviceAccountKey, serviceAccount);
  }

  const databaseURL = process.env["GRAM_DATABASE_URL"];
  if (!databaseURL) {
    throw new Error("GRAM_DATABASE_URL is not set");
  }

  // psql substitutes :'name' variables only in scripts read from stdin, and
  // quotes them as literals.
  const sql = `
INSERT INTO external_credentials (id, organization_id, provider, name)
VALUES (:'credential_id', NULL, 'gcp_iam', 'Local organization encryption')
ON CONFLICT (id) DO UPDATE SET deleted_at = NULL;

INSERT INTO gcp_iam_credentials (external_credential_id, impersonate_service_account, skip_project_verification)
VALUES (:'credential_id', :'service_account', TRUE)
ON CONFLICT (external_credential_id) DO UPDATE
SET impersonate_service_account = EXCLUDED.impersonate_service_account,
    skip_project_verification = TRUE;
`;
  await $({
    input: sql,
    quiet: true,
  })`psql ${databaseURL.replace(/&search_path=.*$/, "")} -v ON_ERROR_STOP=1 -v credential_id=${credentialId} -v service_account=${serviceAccount}`;
  console.log("✅ Local organization encryption credential is in place.");
}

run();
