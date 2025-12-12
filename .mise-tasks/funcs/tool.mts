#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Call a tool on a local Gram Functions runner."
//MISE quiet=true

//USAGE flag "--url <url>" default="http://localhost:8888" help="The base URL of the local Gram Functions runner."
//USAGE flag "--name <name>" required=#true help="The name of the tool to call."
//USAGE flag "--input <json>" help="The JSON input to send to the tool."
//USAGE flag "--env <json>" help="A JSON object of environment variables to use with the tool call."

import assert from "node:assert";
import { Writable } from "node:stream";
import { $ } from "zx";

async function run() {
  const url = process.env["usage_url"];
  const name = process.env["usage_name"];
  const input = process.env["usage_input"]
    ? JSON.parse(process.env["usage_input"])
    : void 0;
  const envVars = process.env["usage_env"]
    ? JSON.parse(process.env["usage_env"])
    : void 0;

  assert(url, "--url argument is required.");
  assert(name, "--name argument is required.");

  const token = await $`mise funcs:mint-v1`.text();

  const res = await fetch(`${url}/tool-call`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token.trim()}`,
    },
    body: JSON.stringify({ name, input, env: envVars }),
  });

  if (!res.ok) {
    const text = await res.text();
    console.error(`Status ${res.status}:\n${text}`);
    process.exit(1);
  }

  if (!res.body) {
    console.log("<No response body>");
    return;
  }

  await res.body.pipeTo(Writable.toWeb(process.stdout));
}

run();
