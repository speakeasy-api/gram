#!/usr/bin/env node

//MISE description="Check local Stripe setup and listener readiness without sending events"
//MISE quiet=true
//USAGE flag "--json" help="Print secret-free machine-readable readiness"
//USAGE flag "--ready" help="Exit nonzero unless the listener is ready"

import { getSetupReadiness } from "./readiness.mts";

const status = await getSetupReadiness();
console.log(
  process.env.usage_json === "true" || process.argv.includes("--json")
    ? JSON.stringify(status)
    : status.checks
        .map((check) => `${check.ok ? "OK" : "ACTION"} ${check.message}`)
        .join("\n"),
);
if (
  (process.env.usage_ready === "true" || process.argv.includes("--ready")) &&
  !(status.listenerReady && status.configurationReady)
)
  process.exitCode = 1;
