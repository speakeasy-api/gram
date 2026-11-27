#!/usr/bin/env node

//MISE description="Prepare private per-worktree HTTPS fixture bindings without printing secrets"
//MISE dir="{{ config_root }}"
//USAGE flag "--dry-run" help="Describe preparation without writing files or starting a process"
//USAGE flag "--port <port>" help="Fixture loopback port; defaults to this worktree's fixture port"
//USAGE flag "--client-port <port>" help="Isolated smoke client loopback port"
//USAGE flag "--cert-file <file>" help="Existing development PEM certificate"
//USAGE flag "--key-file <file>" help="Existing development PEM private key"
//USAGE flag "--ca-file <file>" help="Local root CA for fixture CIMD requests and smoke verification"
//USAGE flag "--bin <file>" help="Absolute executable override for testing an unpushed fixture build"

import { dryRun, envFile, fail, prepare } from "./lib.ts";

try {
  const config = await prepare();
  if (!dryRun)
    console.log(
      `Prepared ${envFile} (0600) for ${config["FIXTURE_BASE_URL"]}; control secret remains private.`,
    );
} catch (error) {
  fail(error);
}
