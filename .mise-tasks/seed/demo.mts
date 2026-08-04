#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Apply the demo workspace SQL seed (seed/demo/) to the local Postgres + ClickHouse stack"
//MISE dir="{{ config_root }}"

import { $ } from "zx";

$.verbose = false;

const DB_USER = process.env.DB_USER || "gram";
const DB_NAME = process.env.DB_NAME || "gram";

async function run() {
  // Postgres: installs demo.ensure_demo_org() and executes it. The function
  // carries its own pre/postflight isolation asserts and raises on violation,
  // which aborts the transaction and fails this task.
  console.log("Applying seed/demo/postgres.sql …");
  await $`docker compose cp seed/demo/postgres.sql gram-db:/tmp/demo-seed.sql`;
  const pgOut =
    await $`docker compose exec -T gram-db psql -U ${DB_USER} -d ${DB_NAME} -v ON_ERROR_STOP=1 -f /tmp/demo-seed.sql`;
  const notice = pgOut.stderr.match(/demo seed ok[^\n]*/)?.[0];
  console.log(`  ${notice ?? "applied"}`);

  // ClickHouse: scoped deletes + inserts; throwIf postflights fail the client
  // with a non-zero exit. `< /dev/null` stops clickhouse-client from blocking
  // on the open exec stdin pipe.
  console.log("Applying seed/demo/clickhouse.sql …");
  await $`docker compose cp seed/demo/clickhouse.sql clickhouse:/tmp/demo-seed.sql`;
  await $`docker compose exec -T clickhouse sh -c ${"clickhouse-client --multiquery --queries-file /tmp/demo-seed.sql < /dev/null"}`;
  console.log("  applied");

  console.log(
    "\nDone. Verify pages with the seed/demo/verify.md playbook (playwright agent),\n" +
      "entering the demo org via the platform-admin impersonation override (org slug: acme-demo).",
  );
}

run().catch((e) => {
  console.error(e.stderr || e.message || e);
  process.exit(1);
});
