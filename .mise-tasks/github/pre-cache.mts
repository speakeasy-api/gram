#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Generate caching information for Go to use in GitHub Actions"
//MISE hide=true

// 💡 It's not possible to use anything other than the Node.js standard library
// because these initialization scripts run _before_ `pnpm install` has run.

import fs from "node:fs/promises";
import crypto from "node:crypto";
import { execSync } from "node:child_process";

if (!process.env["GITHUB_ENV"]) {
  console.error("GITHUB_ENV is not set");
  console.error("Is this running in a GitHub Action?");
  process.exit(1);
}

const env = process.env["GITHUB_ENV"];

async function setupGoCaching() {
  const goBuildCache = execSync("go env GOCACHE", { encoding: "utf8" }).trim();
  const goModCache = execSync("go env GOMODCACHE", { encoding: "utf8" }).trim();

  await fs.appendFile(env, `GOCACHE=${goBuildCache}\n`);
  await fs.appendFile(env, `GOMODCACHE=${goModCache}\n`);

  const os = process.platform;
  const arch = process.arch;

  const hash = crypto.createHash("sha256");

  for (const entry of ["go.mod", "go.sum"]) {
    console.log("Hashing:", entry);
    const goMod = await fs.readFile(entry);
    hash.update(goMod);
  }

  const goModHash = hash.digest("hex");

  const version = 1; // Increment this if you need to bust the cache
  const cacheKey = `${version}-${os}-${arch}-${goModHash}`;
  const partialKey = `${version}-${os}-${arch}-`;
  await fs.appendFile(env, `GH_CACHE_GO_KEY=go-${cacheKey}\n`);
  await fs.appendFile(env, `GH_CACHE_GO_KEY_PARTIAL=go-${partialKey}\n`);

  console.log(`Go cache: ${goBuildCache}`);
  console.log(`Go module cache: ${goModCache}`);
  console.log(`GitHub Go cache key: ${cacheKey}`);
  console.log(`GitHub Go partial cache key: ${partialKey}`);
}

async function setupPNPMCaching() {
  const storePath = execSync("pnpm store path", { encoding: "utf8" }).trim();

  await fs.appendFile(env, `PNPM_STORE_PATH=${storePath}\n`);

  const os = process.platform;
  const arch = process.arch;

  const hash = crypto.createHash("sha256");

  console.log("Hashing:", "pnpm-lock.yaml");
  const pnpmLock = await fs.readFile("pnpm-lock.yaml");
  hash.update(pnpmLock);

  const pnpmHash = hash.digest("hex");

  const version = 1; // Increment this if you need to bust the cache
  const cacheKey = `${version}-${os}-${arch}-${pnpmHash}`;
  const partialKey = `${version}-${os}-${arch}-`;
  await fs.appendFile(env, `GH_CACHE_PNPM_KEY=pnpm-${cacheKey}\n`);
  await fs.appendFile(env, `GH_CACHE_PNPM_KEY_PARTIAL=pnpm-${partialKey}\n`);

  console.log(`PNPM store path: ${storePath}`);
  console.log(`GitHub PNPM cache key: ${cacheKey}`);
  console.log(`GitHub PNPM partial cache key: ${partialKey}`);
}

await setupGoCaching();
await setupPNPMCaching();
