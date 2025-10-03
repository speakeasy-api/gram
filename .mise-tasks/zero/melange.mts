#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Setup a Gram encryption key for local development."
//MISE hide=true

import { $ } from "zx";

async function run() {
  const priv = process.env["MELANGE_PRIVATE_KEY"];
  const pub = process.env["MELANGE_PUBLIC_KEY"];
  const hasPriv = typeof priv === "string" && !!priv && priv !== "unset";
  const hasPub = typeof pub === "string" && !!pub && pub !== "unset";
  if (hasPriv && hasPub) {
    console.log("✅ melange keys are already set.");
    process.exit(0);
  }

  console.log(
    "💬 Melange signing keys will be create to build Gram Functions image locally."
  );

  const key_file = "./local/keys/melange-signing-key.rsa";
  const mise_key_file_path = `${key_file.slice(2)}`;
  await $`mkdir -p ./local/keys`;
  await $`melange keygen ${key_file}`;

  await $`touch mise.local.toml`;
  await $`mise set --file mise.local.toml MELANGE_PRIVATE_KEY='{{config_root}}/${mise_key_file_path}'`;
  await $`mise set --file mise.local.toml MELANGE_PUBLIC_KEY='{{config_root}}/${mise_key_file_path}.pub'`;

  console.log(
    "🔑 MELANGE_PRIVATE_KEY and MELANGE_PUBLIC_KEY have been set in mise.local.toml"
  );
}

run();
