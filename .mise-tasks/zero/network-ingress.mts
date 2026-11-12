#!/usr/bin/env node

//MISE description="Give this developer's local private network artifacts a stable name that cannot collide with production's"
//MISE hide=true
//MISE dir="{{ config_root }}"

import { createHash } from "node:crypto";
import { hostname } from "node:os";
import { dirname } from "node:path";
import { $ } from "zx";

// The local private network joins real tailnets, including ones production
// gateways also serve. With this id the API server, reconciler and gateway
// name everything they create there local-<id>-... (hostnames and services
// svc:local-<id>-<hostname>, machines speakeasy-gw-local-<id>-<slot>), and refuse
// to touch anything else.
const DEV_ID = "GRAM_NETWORK_INGRESS_DEV_ID";

// Four base-36 characters give 1.6 million ids: plenty to keep developers
// apart on a shared tailnet, short enough to read.
const DEV_ID_LENGTH = 4;
const ALPHABET = "abcdefghijklmnopqrstuvwxyz0123456789";

// One id per developer and checkout, shared by every worktree of it. Each new
// hostname costs a new certificate from the tailnet's Let's Encrypt
// allowance (50 a week, shared with everyone on the tailnet), while
// re-issuing an existing name counts as a renewal and does not. So the name
// must not change from worktree to worktree. The developer's git email keeps
// identically provisioned machines, which share a hostname and repository
// path, from colliding; the main checkout's path is the same from every
// worktree. Worktrees of one checkout share the names, so run the private
// network group in one worktree at a time.
async function deriveDevID(): Promise<string> {
  const email = (await $`git config user.email`.nothrow()).stdout.trim();
  const commonDir = (
    await $`git rev-parse --path-format=absolute --git-common-dir`
  ).stdout.trim();
  const digest = createHash("sha256")
    .update(`${email}\0${hostname()}\0${dirname(commonDir)}`)
    .digest();
  let id = "";
  for (let i = 0; i < DEV_ID_LENGTH; i++) {
    id += ALPHABET[digest[i]! % ALPHABET.length];
  }
  return id;
}

async function run(): Promise<void> {
  const current = process.env[DEV_ID];
  if (current) {
    console.log(`✅ ${DEV_ID} is already set (${current}).`);
    return;
  }

  const id = await deriveDevID();
  await $`touch mise.local.toml`;
  await $`mise set --file mise.local.toml ${DEV_ID}=${id}`;
  console.log(
    `🔑 ${DEV_ID}=${id} has been set in mise.local.toml; local private network hostnames are named local-${id}-<hostname> and gateway machines speakeasy-gw-local-${id}-<slot>.`,
  );
}

await run();
