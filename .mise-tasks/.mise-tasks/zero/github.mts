#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Setup GitHub token as part of onboarding"
//MISE hide=true

import { $, question } from "zx";

async function run() {
  const existing = ["MISE_GITHUB_TOKEN", "GITHUB_TOKEN"].find((ev) => {
    const val = process.env[ev];
    return typeof val === "string" && !!val;
  });
  if (existing) {
    console.log(`✅ ${existing} is already set.`);
    return;
  }

  console.log(
    [
      "💬 Mise uses the GitHub API to check for new versions of tools and install them.",
      "Without a GitHub token, it will hit rate limits often and potentially fail.",
      "To fix, create a token WITH NO SCOPES here:",
      "\n\thttps://github.com/settings/tokens/new?description=MISE_GITHUB_TOKEN\n",
    ].join("\n")
  );

  const answer = await question(
    "💬 Paste your GitHub token or press enter to skip: "
  );
  if (!answer) {
    console.log(
      "⚠️ Proceed with caution: A GitHub token will eliminate rate limit errors when working with Mise."
    );
    console.log("⚠️ Run `mise run zero:github` when you have a key to use.");
    return;
  }

  await setKey(answer);
}

async function setKey(value: string) {
  await $`touch mise.local.toml`;
  await $`mise set --file mise.local.toml MISE_GITHUB_TOKEN=${value}`;
  console.log("🔑 MISE_GITHUB_TOKEN has been set in mise.local.toml");
}

run();
