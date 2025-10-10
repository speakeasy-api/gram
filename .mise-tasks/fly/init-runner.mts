#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Create a fly.io app to host Gram Functions runner images"
//MISE hide=true
//USAGE flag "--org <org>" required=#true help="The fly.io organization to create the app in"
//USAGE flag "--image <image>" required=#false help="The image repository to use e.g. registry.fly.io/my-app"
//USAGE flag "--force" default="false" help="Exit with success code if app already exists"

import { $, chalk, fs } from "zx";

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
  const proc = await $`fly apps create --org ${org} ${appName}`.nothrow();
  const exists = proc.stderr.includes("Name has already been taken");
  const fail = proc.exitCode !== 0;
  switch (true) {
    case fail && exists && yn(process.env["usage_force"]):
      console.log(
        `⚠️ App ${appName} already exists, but --force was passed. Continuing.`,
      );
      break;
    case fail:
      console.error("❌ Failed to create fly app:");
      console.error(proc.stderr);
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
