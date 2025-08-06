#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Setup OpenRouter API key as part of onboarding"
//MISE hide=true

import { $, question } from "zx";

async function run() {
  const existing = process.env["OPENROUTER_DEV_KEY"];
  if (typeof existing === "string" && existing !== "unset") {
    console.log("✅ OPENROUTER_DEV_KEY is already set.");
    process.exit(0);
  }

  console.log(
    "💬 OpenRouter API key is needed to perform LLM chat completion calls."
  );
  const env = process.env["OPENROUTER_API_KEY"];
  if (env) {
    const answer = await question(
      "💬 Your environment already has OPENROUTER_API_KEY. Do you want to use it? [y/N] ",
      { choices: ["y", "N"] }
    );

    if (answer.toLowerCase() === "y") {
      await setKey(env);
      process.exit(0);
    }
  }

  console.log(
    "💬 If you don't already have a key, you will need to ask the Gram team for OpenRouter access and create one here:"
  );
  console.log("\thttps://openrouter.ai/settings/keys");
  const answer = await question(
    "💬 Paste your OpenRouter key or press enter to skip: "
  );
  if (!answer) {
    console.log(
      "⚠️ An OpenRouter key is required to complete onboarding. LLM chat will not work until you set one up."
    );
    console.log(
      "⚠️ Run `mise run zero:openrouter` when you have a key to use."
    );
    process.exit(0);
  }

  await setKey(answer);
}

async function setKey(value: string) {
  await $`touch mise.local.toml`;
  await $`mise set --file mise.local.toml OPENROUTER_DEV_KEY=${value}`;
  console.log("🔑 OPENROUTER_DEV_KEY has been set in mise.local.toml");
}

run();
