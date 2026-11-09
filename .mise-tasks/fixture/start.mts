#!/usr/bin/env node

//MISE description="Start only this worktree's opt-in synthetic MCP fixture"
//MISE dir="{{ config_root }}"
//USAGE flag "--dry-run" help="Describe preparation and startup without side effects"
//USAGE flag "--port <port>" help="Fixture loopback port"
//USAGE flag "--client-port <port>" help="Isolated smoke client loopback port"
//USAGE flag "--cert-file <file>" help="Existing development PEM certificate"
//USAGE flag "--key-file <file>" help="Existing development PEM private key"
//USAGE flag "--ca-file <file>" help="Local root CA"
//USAGE flag "--bin <file>" help="Absolute executable override for an unpushed fixture build"

import { execFileSync } from "node:child_process";
import { dryRun, fail, prepare } from "./lib.ts";

try {
  await prepare();
  if (dryRun)
    console.log(
      "Would start the Pitchfork supervisor and only mcp-fixture; the application stack stays as it is.",
    );
  else {
    execFileSync("pitchfork", ["supervisor", "start"], { stdio: "inherit" });
    execFileSync("pitchfork", ["start", "mcp-fixture"], { stdio: "inherit" });
  }
} catch (error) {
  fail(error);
}
