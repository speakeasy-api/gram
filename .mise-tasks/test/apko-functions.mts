#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Run safety tests on an apko-built gram function image"
//MISE dir="{{ config_root }}/functions"

//USAGE flag "--image <image>" required=#true help="The name of the OCI image, including tag."
//USAGE flag "--tarball <tarball>" required=#true help="The path to the OCI tarball"

import * as fs from "node:fs/promises";
import * as path from "node:path";
import assert from "node:assert/strict";
import { $, chalk } from "zx";

const cleanups: Array<() => Promise<void>> = [];

async function createTempZip(): Promise<string> {
  const { stdout } = await $`mktemp -d`;
  const tmpDir = stdout.trim();
  const zipPath = path.join(tmpDir, "functions.zip");

  await $`touch ${path.join(tmpDir, "functions.js")}`;
  await $`touch ${path.join(tmpDir, "functions.py")}`;
  await $`echo '{}' > ${path.join(tmpDir, "manifest.json")}`;

  await $`cd ${tmpDir} && zip ${zipPath} functions.js functions.py manifest.json`;

  cleanups.push(async () => {
    await fs.rm(tmpDir, { recursive: true, force: true });
  });

  return zipPath;
}

async function main() {
  const imageBase = process.env["usage_image"];
  assert(imageBase, "--image is required");
  const tarballPath = process.env["usage_tarball"];
  assert(tarballPath, "--tarball is required");

  if (!imageBase.includes(":")) {
    throw new Error("--image must include a tag");
  }

  let arch: string = process.arch;
  if (arch === "x64") {
    arch = "amd64";
  }

  const image = `${imageBase}-${arch}`;

  let proc = await $`docker images --format json`;
  const existing = proc.stdout.split("\n").find((l) => {
    try {
      const { Repository, Tag } = JSON.parse(l);
      return `${Repository}:${Tag}` === image;
    } catch (e) {
      return false;
    }
  });

  if (existing) {
    console.log(`Image ${image} already exists locally, deleting it.`);
    await $`docker image rm --force ${image}`;
  }

  await $`docker image load -i ${tarballPath}`;
  proc = await $`docker images --format json`;
  const updated = proc.stdout.split("\n").find((l) => {
    try {
      const { Repository, Tag } = JSON.parse(l);
      return `${Repository}:${Tag}` === image;
    } catch (e) {
      return false;
    }
  });
  assert(updated, `Image ${image} not found after loading tarball`);

  const zipPath = await createTempZip();
  const codeBind = `${zipPath}:/data/code.zip`;
  const runScript = containerTester({ image, codeBind });

  let fail = false;
  console.log(chalk.yellowBright(`⏳ Running safety tests on image ${image}`));
  console.log(chalk.yellowBright("1. Checking world-writable directories"));
  let output = await runScript(
    String.raw`find / -type d -perm -0002 ! -perm -1000 -exec ls -ld {} \;`,
  );
  if (output) {
    console.error(chalk.redBright("Found world-writable directories:"));
    console.error(output);
    fail = true;
  } else {
    console.log(chalk.greenBright("No world-writable directories found"));
  }

  console.log(chalk.yellowBright("2. Checking for setuid/setgid files"));
  await runScript(String.raw`find / -perm /6000 -type f`);
  if (output) {
    console.error(chalk.redBright("Found setuid/setgid files:"));
    console.error(output);
    fail = true;
  } else {
    console.log(chalk.greenBright("No setuid/setgid files found"));
  }

  console.log(chalk.yellowBright("3. Checking gram user only owns /home/gram"));
  output = await runScript(String.raw`find / -type d -user gram`);
  if (output !== "/home/gram") {
    console.error(
      chalk.redBright("Found multiple directories owned by gram user:"),
    );
    console.error(output);
    fail = true;
  } else {
    console.log(chalk.greenBright("No directories owned by gram user found"));
  }

  console.log();
  if (fail) {
    console.error(chalk.redBright("❌ One or more safety tests failed"));
    process.exit(1);
  } else {
    console.log(chalk.greenBright("✅ All safety tests passed"));
  }
}

function containerTester(opts: { image: string; codeBind: string }) {
  return async (script: string) => {
    const { image, codeBind } = opts;
    const wrapped = String.raw`
set -e
export GRAM_FUNCTION_AUTH_SECRET="dGVzdC1rZXktMTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM="
export GRAM_PROJECT_ID="019b0849-297e-7d12-85d8-feceb2bee6ef"
export GRAM_DEPLOYMENT_ID="019b0849-7c1f-7b5c-8326-da62cb20aabd"
export GRAM_FUNCTION_ID="019b0849-a74f-7945-9ce1-1f0323b379da"
export GRAM_SERVER_URL="https://localhost:8080"
gram-runner -init -language $RUNNER_LANGUAGE
echo ==START==
${script}
`.trim();
    const proc =
      await $`docker run --entrypoint "" -v ${codeBind} --rm ${image} sh -c ${wrapped}`;
    const output = proc.stdout.split("==START==\n")[1].trim();
    return output;
  };
}

try {
  await main();
} finally {
  const res = await Promise.allSettled(cleanups.map((c) => c()));
  for (const r of res) {
    if (r.status === "rejected") {
      console.error("Cleanup error:", r.reason);
    }
  }
}
