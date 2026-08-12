#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Apply the demo org seed via `gram demo-seed` (embedded SQL from server/internal/demoseed/)"
//MISE dir="{{ config_root }}"

import { $ } from "zx";

$.verbose = true;

// Same code path prod runs: the SQL is go:embedded, the command connects with
// GRAM_DATABASE_URL / CLICKHOUSE_* from the mise environment. The seed's own
// pre/postflight asserts fail the command on any isolation violation.
try {
  await $({ cwd: "server" })`go run . demo-seed`;
} catch (e) {
  const code = (e as { exitCode?: unknown }).exitCode;
  process.exit(typeof code === "number" ? code : 1);
}

console.log(
  "\nDone. Verify pages with the seed/demo/verify.md playbook (playwright agent),\n" +
    "entering the demo org via the platform-admin impersonation override (org slug: acme-demo).",
);
