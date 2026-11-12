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

  const dbUser = process.env["DB_USER"];
  const dbName = process.env["DB_NAME"];
  if (!dbUser || !dbName) {
    throw new Error("DB_USER and DB_NAME must be set");
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
  // psql runs inside the database container, which ./zero has already
  // started, so the host needs no Postgres client.
  await $({
    input: sql,
    quiet: true,
  })`docker compose exec -T gram-db psql -U ${dbUser} -d ${dbName} -v ON_ERROR_STOP=1 -v credential_id=${credentialId} -v service_account=${serviceAccount}`;
  console.log("✅ Local organization encryption credential is in place.");
}

run();
