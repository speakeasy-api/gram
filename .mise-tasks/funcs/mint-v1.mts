#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Create a V1 Bearer token for calling a Gram Functions runner. Combines well with curl and other CLIs."
//MISE quiet=true
//MISE hide=true

//USAGE flag "--key <key>" help="Base64-encoded encryption key."

import { mintV1Bearer } from "./_access.mts";
import assert from "node:assert";

async function main() {
  const key = process.env["usage_key"] || process.env["GRAM_ENCRYPTION_KEY"];
  assert(key, "Usage parameter '--key' is required");

  console.log(await mintV1Bearer(key));
}

main();
