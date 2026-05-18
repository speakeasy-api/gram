#!/usr/bin/env -S node

//MISE description="Setup Gram Functions to use Fly.io during development."
//MISE dir="{{ config_root }}"
//USAGE flag "--restart" default="false" help="Force the onboarding even if configuration already exists."

import os from "node:os";
import process from "node:process";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { $ } from "zx";
import {
  cancel,
  confirm,
  intro,
  isCancel,
  note,
  outro,
  password,
  select,
  spinner,
  text,
} from "@clack/prompts";

$.verbose = false;

type FlyOrg = {
  name: string;
  slug: string;
};

function configValue(content: string, key: string): string | undefined {
  const match = new RegExp(`^\\s*${key}\\s*=\\s*(.+?)\\s*$`, "m").exec(content);
  if (!match) {
    return undefined;
  }

  const value = match[1].trim().replace(/^["']|["']$/g, "");
  return value && value !== "unset" ? value : undefined;
}

function isExistingConfigComplete(): boolean {
  const configPath = join(process.cwd(), "mise.local.toml");

  if (!existsSync(configPath)) {
    return false;
  }

  const content = readFileSync(configPath, "utf-8");
  const requiredKeys = [
    "GRAM_FUNCTIONS_PROVIDER",
    "GRAM_FUNCTIONS_FLYIO_API_TOKEN",
    "GRAM_FUNCTIONS_FLYIO_ORG",
    "GRAM_FUNCTIONS_RUNNER_OCI_IMAGE",
    "GRAM_FUNCTIONS_RUNNER_VERSION",
    "GRAM_FUNCTIONS_FLYIO_REGION",
    "GRAM_ASSISTANT_RUNTIME_PROVIDER",
    "GRAM_ASSISTANT_RUNTIME_FLYIO_API_TOKEN",
    "GRAM_ASSISTANT_RUNTIME_FLYIO_ORG",
    "GRAM_ASSISTANT_RUNTIME_FLYIO_REGION",
    "GRAM_ASSISTANT_RUNTIME_OCI_IMAGE",
    "GRAM_ASSISTANT_RUNTIME_IMAGE_VERSION",
    "GRAM_FUNCTIONS_TIGRIS_BUCKET_URI",
    "GRAM_FUNCTIONS_TIGRIS_KEY",
    "GRAM_FUNCTIONS_TIGRIS_SECRET",
  ];

  if (!requiredKeys.every((key) => configValue(content, key) !== undefined)) {
    return false;
  }

  return (
    configValue(content, "GRAM_FUNCTIONS_PROVIDER") === "flyio" &&
    configValue(content, "GRAM_ASSISTANT_RUNTIME_PROVIDER") === "flyio"
  );
}

function getExisting(...keys: string[]): string | undefined {
  for (const key of keys) {
    const val = process.env[key];
    if (val && val !== "unset") {
      return val;
    }
  }
  return undefined;
}

async function saveConfig(key: string, value: string): Promise<void> {
  await $`mise set --file mise.local.toml ${key}=${value}`;
}

async function saveConfigs(values: Record<string, string>): Promise<void> {
  for (const [key, value] of Object.entries(values)) {
    await saveConfig(key, value);
  }
}

async function fallbackToLocal() {
  await saveConfigs({
    GRAM_FUNCTIONS_PROVIDER: "local",
    GRAM_ASSISTANT_RUNTIME_PROVIDER: "flyio",
    GRAM_ASSISTANT_RUNTIME_FLYIO_ORG: "unset",
    GRAM_ASSISTANT_RUNTIME_FLYIO_API_TOKEN: "unset",
    GRAM_ASSISTANT_RUNTIME_FLYIO_REGION: "us",
    GRAM_ASSISTANT_RUNTIME_OCI_IMAGE: "gram-assistant-runtime",
    GRAM_ASSISTANT_RUNTIME_IMAGE_VERSION: "dev",
  });
  outro(
    "Defaulted Gram Functions to the local provider and wrote boot-safe assistant runtime placeholders. To configure Fly.io later, run `mise run zero:fly --restart`.",
  );
  process.exit(0);
}

function randomAppName() {
  const chars = "abcdefghijklmnopqrstuvwxyz0123456789";
  const bytes = crypto.getRandomValues(new Uint8Array(6));
  const suffix = Array.from(bytes, (b) => chars[b % chars.length]).join("");

  let username = "";
  try {
    username = os.userInfo().username;
  } catch {
    // Fall through to the generic prefix below.
  }

  const user = username
    .toLowerCase()
    .replaceAll(".", "-")
    .replace(/[^a-z0-9-]/g, "-")
    .replace(/^-+|-+$/g, "");
  return `${user || "user"}-${suffix}`;
}

function registryAppName(image: string | undefined): string | undefined {
  if (!image) {
    return undefined;
  }

  const parts = image.split("/");
  return parts.length === 2 && parts[0] === "registry.fly.io"
    ? parts[1]
    : undefined;
}

async function checkFlyLoggedIn(): Promise<{
  loggedIn: boolean;
  email?: string;
}> {
  try {
    const result = await $`fly auth whoami --json`;
    const data = JSON.parse(result.stdout);
    return { loggedIn: true, email: data.email };
  } catch {
    return { loggedIn: false };
  }
}

function parseFlyOrgs(raw: string): FlyOrg[] {
  const data = JSON.parse(raw);
  if (Array.isArray(data)) {
    return data
      .map((item) => ({
        slug: String(item.slug ?? item.Slug ?? item.name ?? item.Name ?? ""),
        name: String(item.name ?? item.Name ?? item.slug ?? item.Slug ?? ""),
      }))
      .filter((org) => org.slug !== "");
  }

  return Object.entries(data as Record<string, string>).map(([slug, name]) => ({
    slug,
    name,
  }));
}

async function listOrgs(token?: string): Promise<FlyOrg[]> {
  const args = token ? ["--access-token", token] : [];
  const result = await $`fly orgs list --json ${args}`;
  return parseFlyOrgs(result.stdout);
}

async function createOrgToken(
  org: string,
  name: string,
  token?: string,
): Promise<string> {
  const args = token ? ["--access-token", token] : [];
  const tokenName = `Local - ${name}`;
  const result =
    await $`fly tokens create org --org ${org} --json --name ${tokenName} ${args}`;
  const data = JSON.parse(result.stdout);
  return data.token;
}

async function validateToken(
  token: string,
): Promise<{ valid: boolean; email?: string }> {
  try {
    const result = await $`fly auth whoami --json --access-token ${token}`;
    const data = JSON.parse(result.stdout);
    return { valid: true, email: data.email };
  } catch {
    return { valid: false };
  }
}

async function listBuckets(org: string, token: string): Promise<string[]> {
  const result = await $`fly storage list --org ${org} --access-token ${token}`;
  const lines = result.stdout.trim().split("\n");
  const buckets: string[] = [];

  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("NAME") || trimmed.startsWith("-")) {
      continue;
    }

    const [bucket] = trimmed.split(/\s+/);
    if (bucket) {
      buckets.push(bucket);
    }
  }

  return buckets;
}

async function openTigrisDashboard(
  bucket: string,
  org: string,
  token: string,
): Promise<void> {
  await $`fly storage dashboard ${bucket} --org ${org} --access-token ${token} --yes`;
}

async function promptForOrg(orgs: FlyOrg[]): Promise<string> {
  if (orgs.length === 0) {
    cancel("No Fly.io organizations found.");
    process.exit(1);
  }

  if (orgs.length === 1) {
    note(`Auto-selected organization: ${orgs[0].slug}`);
    return orgs[0].slug;
  }

  const selectedOrg = await select({
    message: "Select your Fly.io organization",
    options: orgs.map((org) => ({
      value: org.slug,
      label: `${org.name} (${org.slug})`,
    })),
  });
  if (isCancel(selectedOrg)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }

  return selectedOrg;
}

async function getNewToken(
  s: ReturnType<typeof spinner>,
): Promise<{ token: string; org: string }> {
  s.start("Checking Fly.io CLI status...");
  const authStatus = await checkFlyLoggedIn();
  s.stop(
    authStatus.loggedIn
      ? `Logged in as ${authStatus.email ?? "Fly.io user"}`
      : "Not logged in",
  );

  if (authStatus.loggedIn) {
    const useExistingSession = await confirm({
      message:
        "Use your current Fly.io CLI session to create an org-scoped token?",
      active: "Use session",
      inactive: "Enter token",
    });
    if (isCancel(useExistingSession)) {
      cancel("Operation cancelled.");
      process.exit(0);
    }

    if (useExistingSession) {
      s.start("Fetching Fly.io organizations...");
      const orgs = await listOrgs();
      s.stop(`Found ${orgs.length} organization(s)`);
      const org = await promptForOrg(orgs);

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

  s.start("Validating Fly.io token...");
  const validation = await validateToken(token);
  if (!validation.valid) {
    s.stop("Token validation failed");
    cancel("Invalid Fly.io token.");
    process.exit(1);
  }
  s.stop(`Token validated (${validation.email ?? "Fly.io user"})`);

  s.start("Fetching Fly.io organizations...");
  const orgs = await listOrgs(token);
  s.stop(`Found ${orgs.length} organization(s)`);

  const org = await promptForOrg(orgs);
  return { token, org };
}

async function getBucket(
  s: ReturnType<typeof spinner>,
  org: string,
  token: string,
): Promise<string> {
  const existingBucketUri = getExisting("GRAM_FUNCTIONS_TIGRIS_BUCKET_URI");
  const existingBucket = existingBucketUri?.startsWith("s3://")
    ? existingBucketUri.slice("s3://".length)
    : undefined;
  if (existingBucket) {
    note(`Using existing Tigris bucket: ${existingBucket}`);
    return existingBucket;
  }

  s.start("Fetching Tigris buckets...");
  let buckets: string[] = [];
  try {
    buckets = await listBuckets(org, token);
    s.stop(`Found ${buckets.length} bucket(s)`);
  } catch {
    s.stop("Could not list Tigris buckets");
  }

  if (buckets.length === 1) {
    note(`Auto-selected Tigris bucket: ${buckets[0]}`);
    return buckets[0];
  }

  if (buckets.length > 1) {
    const selectedBucket = await select({
      message: "Select your Tigris bucket for Gram Functions",
      options: buckets.map((bucket) => ({ value: bucket, label: bucket })),
    });
    if (isCancel(selectedBucket)) {
      cancel("Operation cancelled.");
      process.exit(0);
    }
    return selectedBucket;
  }

  const bucket = await text({
    message: "Enter your Tigris bucket name for Gram Functions",
    validate: (value) => {
      if (!value) {
        return "Tigris bucket name is required.";
      }
    },
  });
  if (isCancel(bucket)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }

  return bucket;
}

async function getTigrisKey(
  bucket: string,
  org: string,
  token: string,
): Promise<string> {
  const existingTigrisKey = getExisting("GRAM_FUNCTIONS_TIGRIS_KEY");
  if (existingTigrisKey) {
    note("Using existing Tigris Access Key ID");
    return existingTigrisKey;
  }

  let inputKey = await text({
    message: `Enter your Tigris Access Key ID for ${bucket} (leave blank to open dashboard)`,
    validate: (value) => {
      if (!value) {
        return;
      }
      if (!value.startsWith("tid_")) {
        return "Invalid Tigris Access Key ID. It should start with 'tid_'.";
      }
    },
  });
  if (isCancel(inputKey)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }

  if (!inputKey) {
    note("Opening Tigris dashboard to create an access key...");
    try {
      await openTigrisDashboard(bucket, org, token);
    } catch {
      note("Could not open the Tigris dashboard automatically.");
    }

    inputKey = await text({
      message: "Enter the Tigris Access Key ID you created",
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
  }

  return inputKey;
}

async function getTigrisSecret(bucket: string): Promise<string> {
  const existingTigrisSecret = getExisting("GRAM_FUNCTIONS_TIGRIS_SECRET");
  if (existingTigrisSecret) {
    note("Using existing Tigris Secret Access Key");
    return existingTigrisSecret;
  }

  const tigrisSecret = await password({
    message: `Enter your Tigris Secret Access Key for ${bucket}`,
    validate: (value) => {
      if (!value?.startsWith("tsec_")) {
        return "Invalid Tigris Secret Access Key. It should start with 'tsec_'.";
      }
    },
  });
  if (isCancel(tigrisSecret)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }

  return tigrisSecret;
}

async function run() {
  if (isExistingConfigComplete() && process.env["usage_restart"] !== "true") {
    console.log(
      "Fly.io configuration already exists in mise.local.toml. To start onboarding again, run `mise run zero:fly --restart`.",
    );
    process.exit(0);
  }

  intro("Gram Functions Fly.io Setup");

  note(
    `
To deploy Gram Functions and assistant runtimes to Fly.io, you'll need:
    - A Fly.io account
    - A Fly.io organization-scoped token
    - A Fly.io app namespace for runner images
    - A Tigris bucket associated with the Fly.io organization
    - A Tigris Access Key ID and Secret Access Key with bucket access
`.slice(1, -1),
    "Pre-requisites",
  );

  const proceed = await confirm({
    message: "Set up Fly.io and Tigris",
    active: "Start",
    inactive: "Skip",
  });
  if (isCancel(proceed)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }
  if (!proceed) {
    await fallbackToLocal();
  }

  const s = spinner();
  const existingToken = getExisting(
    "GRAM_FUNCTIONS_FLYIO_API_TOKEN",
    "GRAM_ASSISTANT_RUNTIME_FLYIO_API_TOKEN",
  );
  const existingOrg = getExisting(
    "GRAM_FUNCTIONS_FLYIO_ORG",
    "GRAM_ASSISTANT_RUNTIME_FLYIO_ORG",
  );

  let token: string;
  let org: string;
  if (existingToken && existingOrg) {
    s.start("Validating existing Fly.io token...");
    const validation = await validateToken(existingToken);
    if (validation.valid) {
      s.stop(`Using existing token (${validation.email ?? "Fly.io user"})`);
      token = existingToken;
      org = existingOrg;
    } else {
      s.stop("Existing token invalid");
      const result = await getNewToken(s);
      token = result.token;
      org = result.org;
    }
  } else {
    const result = await getNewToken(s);
    token = result.token;
    org = result.org;
  }

  const initialApp =
    registryAppName(getExisting("GRAM_FUNCTIONS_RUNNER_OCI_IMAGE")) ??
    registryAppName(getExisting("GRAM_ASSISTANT_RUNTIME_OCI_IMAGE")) ??
    randomAppName();
  const app = await text({
    message:
      "Enter your Fly.io app name for runner images (accept the default if unsure)",
    initialValue: initialApp,
    validate: (value) => {
      if (!value) {
        return "Fly.io app name is required.";
      }
    },
  });
  if (isCancel(app)) {
    cancel("Operation cancelled.");
    process.exit(0);
  }

  const bucket = await getBucket(s, org, token);
  const tigrisKey = await getTigrisKey(bucket, org, token);
  const tigrisSecret = await getTigrisSecret(bucket);

  await saveConfigs({
    GRAM_FUNCTIONS_PROVIDER: "flyio",
    GRAM_FUNCTIONS_FLYIO_ORG: org,
    GRAM_FUNCTIONS_FLYIO_API_TOKEN: token,
    GRAM_FUNCTIONS_RUNNER_OCI_IMAGE: `registry.fly.io/${app}`,
    GRAM_FUNCTIONS_RUNNER_VERSION: "main",
    GRAM_FUNCTIONS_FLYIO_REGION: "us",
    GRAM_ASSISTANT_RUNTIME_PROVIDER: "flyio",
    GRAM_ASSISTANT_RUNTIME_FLYIO_ORG: org,
    GRAM_ASSISTANT_RUNTIME_FLYIO_API_TOKEN: token,
    GRAM_ASSISTANT_RUNTIME_FLYIO_REGION: "us",
    GRAM_ASSISTANT_RUNTIME_OCI_IMAGE: `registry.fly.io/${app}`,
    GRAM_ASSISTANT_RUNTIME_IMAGE_VERSION: "dev",
    GRAM_FUNCTIONS_TIGRIS_BUCKET_URI: `s3://${bucket}`,
    GRAM_FUNCTIONS_TIGRIS_KEY: tigrisKey,
    GRAM_FUNCTIONS_TIGRIS_SECRET: tigrisSecret,
  });

  outro(
    "Updated mise.local.toml. You're ready to deploy Gram Functions and assistant runtimes to Fly.io.",
  );
}

await run();
