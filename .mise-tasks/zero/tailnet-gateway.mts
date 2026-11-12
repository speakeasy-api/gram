#!/usr/bin/env node

//MISE description="Give this worktree's local tailnet gateway devices a name that cannot be mistaken for production's"
//MISE hide=true
//MISE dir="{{ config_root }}"

import { createHash } from "node:crypto";
import { hostname } from "node:os";
import { $ } from "zx";

// The local gateway joins real tailnets, including ones production gateways
// also serve. The start task names its devices speakeasy-gw-dev-<id>-<slot>
// instead of production's speakeasy-gw-<slot>, using this id.
const DEV_ID = "GRAM_TAILNET_GATEWAY_DEV_ID";

// Four base-36 characters give 1.6 million ids: plenty to keep every
// developer's worktrees apart on a shared tailnet, short enough to read.
const DEV_ID_LENGTH = 4;
const ALPHABET = "abcdefghijklmnopqrstuvwxyz0123456789";

// Derived from the machine and the worktree path, so rerunning on the same
// worktree gives the same id and its devices keep their names, while another
// worktree or machine gets a different one.
function deriveDevID(): string {
  const digest = createHash("sha256")
    .update(`${hostname()}\0${process.cwd()}`)
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

  const id = deriveDevID();
  await $`touch mise.local.toml`;
  await $`mise set --file mise.local.toml ${DEV_ID}=${id}`;
  console.log(
    `🔑 ${DEV_ID}=${id} has been set in mise.local.toml; local gateway devices are named speakeasy-gw-dev-${id}-<slot>.`,
  );
}

await run();
