#!/usr/bin/env -S node

//MISE description="Generate caching information for Go to use in GitHub Actions"
//MISE hide=true

// 💡 It's not possible to use anything other than the Node.js standard library
// because these initialization scripts run _before_ `aube install` has run.

import fs from "node:fs/promises";
import crypto from "node:crypto";
import { execSync } from "node:child_process";

if (!process.env["GITHUB_ENV"]) {
  console.error("GITHUB_ENV is not set");
  console.error("Is this running in a GitHub Action?");
  process.exit(1);
}

const env = process.env["GITHUB_ENV"];

// Blacksmith transparently handles actions/cache on its runners, but a
// GitHub-hosted workflow can populate the same default-branch key in GitHub's
// backend. Keep the namespaces distinct so an exact GitHub-backed hit cannot
// prevent a colocated entry from being populated on Blacksmith.
const configuredCacheNamespace = process.env["CACHE_NAMESPACE"];

if (
  process.env["RUNNER_ENVIRONMENT"] === "github-hosted" &&
  !configuredCacheNamespace
) {
  console.error(
    "CACHE_NAMESPACE must be set explicitly on GitHub-hosted runners",
  );
  process.exit(1);
}

const cacheNamespace = configuredCacheNamespace || "blacksmith-v1";

if (!/^[A-Za-z0-9._-]+$/.test(cacheNamespace)) {
  console.error(
    "CACHE_NAMESPACE may only contain letters, numbers, dots, underscores, and hyphens",
  );
  process.exit(1);
}

async function setupGoCaching() {
  const goBuildCache = execSync("go env GOCACHE", { encoding: "utf8" }).trim();
  const goModCache = execSync("go env GOMODCACHE", { encoding: "utf8" }).trim();
  const goVersion = execSync("go env GOVERSION", { encoding: "utf8" }).trim();

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

  const version = 1; // Increment this to bust the Go cache
  const cacheKey = `${cacheNamespace}-${version}-${os}-${arch}-${goVersion}-${goModHash}`;
  const partialKey = `${cacheNamespace}-${version}-${os}-${arch}-${goVersion}-`;
  await fs.appendFile(env, `GH_CACHE_GO_KEY=go-${cacheKey}\n`);
  await fs.appendFile(env, `GH_CACHE_GO_KEY_PARTIAL=go-${partialKey}\n`);

  console.log(`Go cache: ${goBuildCache}`);
  console.log(`Go module cache: ${goModCache}`);
  console.log(`GitHub Go cache key: ${cacheKey}`);
  console.log(`GitHub Go partial cache key: ${partialKey}`);
}

async function setupUVCaching() {
  const uvVersion = execSync("uv --version", { encoding: "utf8" })
    .trim()
    .split(/\s+/)[1]; // "uv 0.11.21" -> "0.11.21"

  // Keep the cache dir deterministic and inside the workspace so the
  // actions/cache step and `uv sync` agree on the location.
  const cacheDir = `${process.env["GITHUB_WORKSPACE"]}/.uv-cache`;
  await fs.appendFile(env, `UV_CACHE_DIR=${cacheDir}\n`);

  const os = process.platform;
  const arch = process.arch;

  const hash = crypto.createHash("sha256");

  // Single workspace-wide lockfile: any member's dependency change busts it.
  console.log("Hashing:", "uv.lock");
  const uvLock = await fs.readFile("uv.lock");
  hash.update(uvLock);

  const uvHash = hash.digest("hex");

  const version = 1; // Increment this to bust the uv cache
  const cacheKey = `${cacheNamespace}-${version}-${os}-${arch}-uv${uvVersion}-${uvHash}`;
  const partialKey = `${cacheNamespace}-${version}-${os}-${arch}-uv${uvVersion}-`;
  await fs.appendFile(env, `GH_CACHE_UV_KEY=uv-${cacheKey}\n`);
  await fs.appendFile(env, `GH_CACHE_UV_KEY_PARTIAL=uv-${partialKey}\n`);

  console.log(`uv cache dir: ${cacheDir}`);
  console.log(`GitHub uv cache key: ${cacheKey}`);
  console.log(`GitHub uv partial cache key: ${partialKey}`);
}

async function setupAubeCaching() {
  // Only the content-addressable store is worth caching here: aube disables its
  // global virtual store under CI, so nothing is materialized outside the
  // workspace's own node_modules.
  const storePath = execSync("aube store path", { encoding: "utf8" }).trim();
  const aubeMajorVersion = execSync("aube --version", { encoding: "utf8" })
    .trim()
    .split(".")[0];

  await fs.appendFile(env, `AUBE_STORE_PATH=${storePath}\n`);

  const os = process.platform;
  const arch = process.arch;

  const hash = crypto.createHash("sha256");

  // aube reads and writes pnpm-lock.yaml in place, so the lockfile keeps its
  // pnpm name and stays the cache-busting input.
  console.log("Hashing:", "pnpm-lock.yaml");
  const lockfile = await fs.readFile("pnpm-lock.yaml");
  hash.update(lockfile);

  const lockfileHash = hash.digest("hex");

  const version = 1; // Increment this to bust the aube cache
  const cacheKey = `${cacheNamespace}-${version}-${os}-${arch}-aube${aubeMajorVersion}-${lockfileHash}`;
  const partialKey = `${cacheNamespace}-${version}-${os}-${arch}-aube${aubeMajorVersion}-`;
  await fs.appendFile(env, `GH_CACHE_AUBE_KEY=aube-${cacheKey}\n`);
  await fs.appendFile(env, `GH_CACHE_AUBE_KEY_PARTIAL=aube-${partialKey}\n`);

  console.log(`aube store path: ${storePath}`);
  console.log(`GitHub aube cache key: ${cacheKey}`);
  console.log(`GitHub aube partial cache key: ${partialKey}`);
}

await setupGoCaching();
await setupUVCaching();
await setupAubeCaching();
