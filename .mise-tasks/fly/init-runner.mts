#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Create a fly.io app to host Gram Functions runner images"
//MISE hide=true
//USAGE flag "--org <org>" help="The fly.io organization to create the app in" required=#true
//USAGE flag "--name <name>" help="The name of the runner" required=#true
//USAGE flag "--image-prefix <prefix>" help="The image prefix to use" required=#false
//USAGE flag "--force" help="Exit with success code if app already exists" default="false"

import { $, fs } from "zx";

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

async function main() {
  const accessToken = process.env["FLY_API_TOKEN"];
  if (!accessToken) {
    console.error("❌ FLY_API_TOKEN is required.");
    return process.exit(1);
  }

  const prefix =
    process.env["usage_image_prefix"] ?? process.env["FLY_IMAGE_PREFIX"] ?? "";
  if (!prefix) {
    console.error("❌ --prefix or FLY_IMAGE_PREFIX are required.");
    return process.exit(1);
  }

  const name = process.env["usage_name"] ?? "";

  const org = process.env["usage_org"];
  const fullName = `${prefix}-runner-${name}`;

  console.log(`ℹ️ Creating fly app in ${org}: ${fullName}`);
  const proc = await $`fly apps create --org ${org} ${fullName}`.nothrow();
  const exists = proc.stderr.includes("Name has already been taken");
  const fail = proc.exitCode !== 0;
  switch (true) {
    case fail && exists && yn(process.env["usage_force"]):
      console.log(
        `⚠️ App ${fullName} already exists, but --force was passed. Continuing.`
      );
      break;
    case fail:
      console.error("❌ Failed to create fly app:");
      console.error(proc.stderr);
      return process.exit(1);
    default:
      console.log(`✅ Fly app ${fullName} created in ${org}.`);
  }

  const outputs = process.env["GITHUB_OUTPUT"];
  if (outputs) {
    console.log("ℹ️ Setting outputs for GitHub Actions");
    await fs.appendFile(outputs, `flyAppName=${fullName}\n`);
    await fs.appendFile(
      outputs,
      `flyAppRegistry=registry.fly.io/${fullName}\n`
    );
  }
}

main();
