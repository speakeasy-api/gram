#!/usr/bin/env node

//MISE description="Stop only this worktree's MCP fixture"
//MISE dir="{{ config_root }}"
//USAGE flag "--dry-run" help="Describe shutdown without stopping any process"

import { execFileSync } from "node:child_process";
import { dryRun, fail } from "./lib.ts";

try {
  if (dryRun)
    console.log("Would run pitchfork stop mcp-fixture for this worktree only.");
  else execFileSync("pitchfork", ["stop", "mcp-fixture"], { stdio: "inherit" });
} catch (error) {
  fail(error);
}
