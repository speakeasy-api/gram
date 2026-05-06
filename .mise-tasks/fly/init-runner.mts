#!/usr/bin/env -S node

//MISE description="Create a fly.io app to host Gram Functions runner images"
//MISE hide=true
//USAGE flag "--org <org>" required=#true help="The fly.io organization to create the app in"
//USAGE flag "--image <image>" required=#false help="The image repository to use e.g. registry.fly.io/my-app"
//USAGE flag "--force" default="false" help="Exit with success code if app already exists"

import { $, chalk, fs, sleep } from "zx";
import type { ProcessOutput } from "zx";

const MAX_ATTEMPTS = 5;
const BASE_DELAY_MS = 2000;
const MAX_DELAY_MS = 30000;

const TRANSIENT_PATTERNS = [
  /non-200 status code: 5\d{2}/i,
  /context deadline exceeded/i,
  /i\/o timeout/i,
  /connection (reset|refused|closed)/i,
  /unexpected EOF/i,
  /no such host/i,
  /TLS handshake/i,
  /temporarily unavailable/i,
  /bad gateway/i,
  /gateway timeout/i,
  /service unavailable/i,
];

function isTransient(output: string): boolean {
  return TRANSIENT_PATTERNS.some((p) => p.test(output));
}

function backoffMs(attempt: number): number {
  const exp = Math.min(BASE_DELAY_MS * 2 ** (attempt - 1), MAX_DELAY_MS);
  return exp + Math.floor(Math.random() * 500);
}

function yn(val: string | undefined) {
  const y = new Set(["1", "t", "T", "true", "TRUE", "True"]);
  if (val === undefined) {
    return false;
  }
  const lower = val.toLowerCase();
  if (y.has(lower)) {
    return true;
  }

  return false;
}

function parseImageName(image: string): [registry: string, appName: string] {
  const parts = image.split("/");
  halt(
    parts.length === 2,
    `Expected image name to be in the format <registry>/<appName>: ${image}`,
  );

  const registry = parts[0];
  halt(
    registry === "registry.fly.io",
    `Expected registry to be registry.fly.io, got ${registry}`,
  );

  const appName = parts[1];
  halt(appName, "App name cannot be empty");

  return [registry, appName];
}

function halt(condition: unknown, message: string): asserts condition {
  if (!condition) {
    console.error(chalk.redBright(`❌ ${message}`));
    process.exit(1);
  }
}

async function main() {
  const accessToken = process.env["FLY_API_TOKEN"];
  halt(accessToken, "FLY_API_TOKEN is required.");

  const image = process.env["usage_image"] ?? process.env["FLY_IMAGE"] ?? "";
  halt(image, "--image or FLY_IMAGE are required.");

  const org = process.env["usage_org"];
  halt(org, "--org is required.");

  const [, appName] = parseImageName(image);

  console.log(`ℹ️ Creating fly app in ${org}: ${appName}`);

  let proc: ProcessOutput | undefined;
  let combined = "";
  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
    proc = await $`fly apps create --org ${org} ${appName}`.nothrow();
    combined = `${proc.stderr}\n${proc.stdout}`;
    if (proc.exitCode === 0) break;
    if (combined.includes("Name has already been taken")) break;

    if (attempt < MAX_ATTEMPTS && isTransient(combined)) {
      const delay = backoffMs(attempt);
      console.warn(
        chalk.yellow(
          `⚠️ Attempt ${attempt}/${MAX_ATTEMPTS} failed with transient error; retrying in ${delay}ms.`,
        ),
      );
      await sleep(delay);
      continue;
    }

    break;
  }

  halt(proc, "fly apps create did not run");

  const exists = combined.includes("Name has already been taken");
  const fail = proc.exitCode !== 0;
  switch (true) {
    case fail && exists && yn(process.env["usage_force"]):
      console.log(
        `⚠️ App ${appName} already exists, but --force was passed. Continuing.`,
      );
      break;
    case fail:
      console.error("❌ Failed to create fly app:");
      console.error(combined);
      return process.exit(1);
    default:
      console.log(`✅ Fly app ${appName} created in ${org}.`);
  }

  const outputs = process.env["GITHUB_OUTPUT"];
  if (outputs) {
    console.log("ℹ️ Setting outputs for GitHub Actions");
    await fs.appendFile(outputs, `flyAppName=${appName}\n`);
    await fs.appendFile(outputs, `flyAppRegistry=${image}\n`);
  }
}

main();
