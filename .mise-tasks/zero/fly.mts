#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Setup Gram Functions to use Fly.io during development."
//MISE dir="{{ config_root }}"
//USAGE flag "--restart" default="false" help="Force the onboarding even if configuration already exists."

import process from "node:process";
import os from "node:os";
import { $ } from "zx";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import {
  intro,
  note,
  outro,
  confirm,
  isCancel,
  cancel,
  text,
  password,
  select,
  spinner,
} from "@clack/prompts";

$.verbose = false;

interface FlyOrg {
  name: string;
  slug: string;
}

function checkExistingConfig(): { exists: boolean; isComplete: boolean } {
  const configPath = join(process.cwd(), "mise.local.toml");

  if (!existsSync(configPath)) {
    return { exists: false, isComplete: false };
  }

  const content = readFileSync(configPath, "utf-8");

  const requiredKeys = [
    "GRAM_FUNCTIONS_PROVIDER",
    "GRAM_FUNCTIONS_FLYIO_API_TOKEN",
    "GRAM_FUNCTIONS_FLYIO_ORG",
    "GRAM_FUNCTIONS_RUNNER_OCI_IMAGE",
    "GRAM_FUNCTIONS_TIGRIS_BUCKET_URI",
    "GRAM_FUNCTIONS_TIGRIS_KEY",
    "GRAM_FUNCTIONS_TIGRIS_SECRET",
  ];

  const isComplete = requiredKeys.every((key) =>
    new RegExp(`^\\s*${key}\\s*=`, "gm").test(content)
  );

  return { exists: true, isComplete };
}

async function checkFlyLoggedIn(): Promise<{ loggedIn: boolean; email?: string }> {
  try {
    const result = await $`fly auth whoami --json`;
    const data = JSON.parse(result.stdout);
    return { loggedIn: true, email: data.email };
  } catch {
    return { loggedIn: false };
  }
}

async function listOrgs(token?: string): Promise<FlyOrg[]> {
  const args = token ? ["-t", token] : [];
  const result = await $`fly orgs list --json ${args}`;
  const data = JSON.parse(result.stdout) as Record<string, string>;
  return Object.entries(data).map(([slug, name]) => ({ slug, name }));
}

async function createOrgToken(org: string, name: string, token?: string): Promise<string> {
  const args = token ? ["-t", token] : [];
  const tokenName = `Local - ${name}`;
  const result = await $`fly tokens create org -o ${org} --json -n ${tokenName} ${args}`;
  const data = JSON.parse(result.stdout);
  return data.token;
}

async function validateToken(token: string): Promise<{ valid: boolean; email?: string }> {
  try {
    const result = await $`fly auth whoami --json -t ${token}`;
    const data = JSON.parse(result.stdout);
    return { valid: true, email: data.email };
  } catch {
    return { valid: false };
  }
}

async function listBuckets(org: string, token: string): Promise<string[]> {
  // fly storage list doesn't support --json, so we parse table output
  const result = await $`fly storage list -o ${org} -t ${token}`;
  const lines = result.stdout.trim().split("\n");
  const buckets: string[] = [];
  for (const line of lines) {
    if (!line.trim() || line.startsWith("NAME") || line.startsWith("-")) {
      continue;
    }
    const parts = line.trim().split(/\s+/);
    if (parts[0]) {
      buckets.push(parts[0]);
    }
  }
  return buckets;
}

async function openTigrisDashboard(bucket: string, org: string, token: string): Promise<void> {
  await $`fly storage dashboard ${bucket} -o ${org} -t ${token} --yes`;
}

function getExisting(key: string): string | undefined {
  const val = process.env[key];
  return val && val !== "unset" ? val : undefined;
}

async function saveConfig(key: string, value: string): Promise<void> {
  await $`mise set --file mise.local.toml ${key}=${value}`;
}

async function run() {
  if (
    checkExistingConfig().isComplete &&
    process.env["usage_restart"] !== "true"
  ) {
    console.log(
      "GRAM_FUNCTIONS_PROVIDER already configured in mise.local.toml. To start fly.io onboarding again, run `mise run zero:fly --restart`.",
    );
    process.exit(0);
  }

  intro(`Gram Functions Fly.io Setup`);

  note(
    `
To deploy Gram Functions to Fly.io, you'll need:
    - A Fly.io account (https://fly.io)
    - A Fly.io app hosting the Gram Functions runner images
    - A Tigris bucket associated with the Fly.io organization
    - A Tigris Access Key ID and Secret Access Key with permissions to access the bucket
`.slice(1, -1),
    "Pre-requisites",
  );

  const proceed = await confirm({ message: "Are you ready to proceed?" });
  if (isCancel(proceed)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }
  if (!proceed) {
    const args = ["GRAM_FUNCTIONS_PROVIDER=local"];
    await $`mise set --file mise.local.toml ${args}`;

    outro(
      "Defaulted to stubbed local provider. To start this onboarding again, run `mise run zero:fly --restart`.",
    );
    process.exit(0);
  }

  const s = spinner();

  const existingToken = getExisting("GRAM_FUNCTIONS_FLYIO_API_TOKEN");
  const existingOrg = getExisting("GRAM_FUNCTIONS_FLYIO_ORG");

  let token: string;
  let org: string;

  if (existingToken && existingOrg) {
    s.start("Validating existing token...");
    const validation = await validateToken(existingToken);
    if (validation.valid) {
      s.stop(`Using existing token (${validation.email})`);
      token = existingToken;
      org = existingOrg;
    } else {
      s.stop("Existing token invalid, need new token");
      const result = await getNewToken(s);
      token = result.token;
      org = result.org;
    }
  } else {
    const result = await getNewToken(s);
    token = result.token;
    org = result.org;
  }

  await saveConfig("GRAM_FUNCTIONS_PROVIDER", "flyio");
  await saveConfig("GRAM_FUNCTIONS_FLYIO_API_TOKEN", token);
  await saveConfig("GRAM_FUNCTIONS_FLYIO_ORG", org);
  await saveConfig("GRAM_FUNCTIONS_FLYIO_REGION", "us");

  const existingApp = getExisting("GRAM_FUNCTIONS_RUNNER_OCI_IMAGE")?.split("/")[1];
  let app: string;

  if (existingApp) {
    note(`Using existing registry app: ${existingApp}`);
    app = existingApp;
  } else {
    const hostname = os.hostname().toLowerCase().replace(/[^a-z0-9-]/g, "-").replace(/^-+|-+$/g, "");
    app = `local-${hostname}`;
    note(`Using registry app: ${app}`);
  }

  await saveConfig("GRAM_FUNCTIONS_RUNNER_OCI_IMAGE", `registry.fly.io/${app}`);
  await saveConfig("GRAM_FUNCTIONS_RUNNER_VERSION", "main");

  const existingBucketUri = getExisting("GRAM_FUNCTIONS_TIGRIS_BUCKET_URI");
  const existingBucket = existingBucketUri?.startsWith("s3://") ? existingBucketUri.slice(5) : undefined;
  let bucket: string;

  if (existingBucket) {
    note(`Using existing bucket: ${existingBucket}`);
    bucket = existingBucket;
  } else {
    s.start("Fetching Tigris buckets...");
    const buckets = await listBuckets(org, token);
    s.stop(`Found ${buckets.length} bucket(s)`);

    if (buckets.length === 0) {
      cancel("No Tigris buckets found. Create one at https://console.tigris.dev or run `fly storage create`");
      process.exit(1);
    }

    if (buckets.length === 1) {
      bucket = buckets[0];
      note(`Auto-selected bucket: ${bucket}`);
    } else {
      const selectedBucket = await select({
        message: "Select your Tigris bucket for Gram Functions",
        options: buckets.map((b) => ({ value: b, label: b })),
      });
      if (isCancel(selectedBucket)) {
        cancel("Operation cancelled.");
        process.exit(0);
      }
      bucket = selectedBucket;
    }
  }

  await saveConfig("GRAM_FUNCTIONS_TIGRIS_BUCKET_URI", `s3://${bucket}`);

  const existingTigrisKey = getExisting("GRAM_FUNCTIONS_TIGRIS_KEY");
  let tigrisKey: string;

  if (existingTigrisKey) {
    note(`Using existing Tigris Access Key ID`);
    tigrisKey = existingTigrisKey;
  } else {
    let inputKey = await text({
      message: `Enter your Tigris Access Key ID for ${bucket} (leave blank to open dashboard)`,
    });
    if (isCancel(inputKey)) {
      cancel("Operation cancelled.");
      process.exit(0);
    }

    if (!inputKey) {
      note("Opening Tigris dashboard to create an access key...");
      await openTigrisDashboard(bucket, org, token);

      inputKey = await text({
        message: "Enter the Tigris Access Key ID you just created",
        validate: (value) => {
          if (!value?.startsWith("tid_")) {
            return "Invalid Tigris Access Key ID. It should start with 'tid_'.";
          }
        },
      });
      if (isCancel(inputKey)) {
        cancel("Operation cancelled.");
        process.exit(0);
      }
    } else if (!inputKey.startsWith("tid_")) {
      cancel("Invalid Tigris Access Key ID. It should start with 'tid_'.");
      process.exit(1);
    }
    tigrisKey = inputKey;
  }

  await saveConfig("GRAM_FUNCTIONS_TIGRIS_KEY", tigrisKey);

  const existingTigrisSecret = getExisting("GRAM_FUNCTIONS_TIGRIS_SECRET");
  let tigrisSecret: string;

  if (existingTigrisSecret) {
    note(`Using existing Tigris Secret Access Key`);
    tigrisSecret = existingTigrisSecret;
  } else {
    const inputSecret = await password({
      message: `Enter your Tigris Secret Access Key for ${bucket}`,
      validate: (value) => {
        if (!value?.startsWith("tsec_")) {
          return "Invalid Tigris Secret Access Key. It should start with 'tsec_'.";
        }
      },
    });
    if (isCancel(inputSecret)) {
      cancel("Operation cancelled.");
      process.exit(0);
    }
    tigrisSecret = inputSecret;
  }

  await saveConfig("GRAM_FUNCTIONS_TIGRIS_SECRET", tigrisSecret);

  outro(
    `Updated mise.local.toml. You're ready to deploy Gram Functions to Fly.io!`,
  );
}

async function getNewToken(s: ReturnType<typeof spinner>): Promise<{ token: string; org: string }> {
  s.start("Checking Fly.io CLI status...");
  const authStatus = await checkFlyLoggedIn();
  s.stop(authStatus.loggedIn ? `Logged in as ${authStatus.email}` : "Not logged in");

  if (authStatus.loggedIn) {
    const useExisting = await confirm({
      message: "Use your current Fly.io CLI session to create an org-scoped token?",
    });
    if (isCancel(useExisting)) {
      cancel("Operation cancelled.");
      process.exit(0);
    }

    if (useExisting) {
      s.start("Fetching organizations...");
      const orgs = await listOrgs();
      s.stop(`Found ${orgs.length} organization(s)`);

      if (orgs.length === 0) {
        cancel("No organizations found. Create one at https://fly.io/dashboard");
        process.exit(1);
      }

      let org: string;
      if (orgs.length === 1) {
        org = orgs[0].slug;
        note(`Auto-selected organization: ${org}`);
      } else {
        const selectedOrg = await select({
          message: "Select your Fly.io organization",
          options: orgs.map((o) => ({ value: o.slug, label: `${o.name} (${o.slug})` })),
        });
        if (isCancel(selectedOrg)) {
          cancel("Operation cancelled.");
          process.exit(0);
        }
        org = selectedOrg;
      }

      s.start("Creating organization-scoped token...");
      const token = await createOrgToken(org, os.hostname());
      s.stop("Token created");

      return { token, org };
    }
  }

  const token = await password({
    message: "Enter your Fly.io organization-scoped token",
    validate: (value) => {
      if (!value?.startsWith("FlyV1 ")) {
        return "Invalid Fly.io token. It should start with 'FlyV1 ...'.";
      }
    },
  });
  if (isCancel(token)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }

  s.start("Validating token...");
  const validation = await validateToken(token);
  if (!validation.valid) {
    s.stop("Token validation failed");
    cancel("Invalid Fly.io token.");
    process.exit(1);
  }
  s.stop(`Token validated (${validation.email})`);

  s.start("Fetching organizations...");
  const orgs = await listOrgs(token);
  s.stop(`Found ${orgs.length} organization(s)`);

  let org: string;
  if (orgs.length === 0) {
    cancel("No organizations found for this token.");
    process.exit(1);
  } else if (orgs.length === 1) {
    org = orgs[0].slug;
    note(`Auto-selected organization: ${org}`);
  } else {
    const selectedOrg = await select({
      message: "Select your Fly.io organization",
      options: orgs.map((o) => ({ value: o.slug, label: `${o.name} (${o.slug})` })),
    });
    if (isCancel(selectedOrg)) {
      cancel("Operation cancelled.");
      process.exit(0);
    }
    org = selectedOrg;
  }

  return { token, org };
}

await run();
