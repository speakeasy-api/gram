#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Start/restart worktree-managed Stripe sandbox webhook forwarding"
//MISE quiet=true

import { execFileSync } from "node:child_process";
import {
  persistedStripeConfig,
  registerStripeListener,
  stripeConfigIssue,
} from "./helpers.mts";

try {
  if (process.env.GRAM_ENVIRONMENT !== "local")
    throw new Error("Stripe forwarding requires GRAM_ENVIRONMENT=local.");
  let config;
  try {
    config = persistedStripeConfig();
  } catch {
    throw new Error(
      "Cannot read local Stripe configuration. Check the loopback GRAM_SERVER_URL and GRAM_SERVER_PORT; run mise run stripe:setup before mise run stripe:listen.",
    );
  }
  // Registration is the opt-in; validate saved credentials before creating it.
  const issue = stripeConfigIssue({ ...config, enabled: true });
  if (issue) throw new Error(issue);
  registerStripeListener();
  try {
    execFileSync("pitchfork", ["supervisor", "start"], {
      stdio: "pipe",
      timeout: 30_000,
      killSignal: "SIGKILL",
    });
    // Force a fresh mise environment even when forwarding is already running.
    execFileSync("pitchfork", ["start", "stripe-listener", "--force"], {
      stdio: "pipe",
      timeout: 120_000,
      killSignal: "SIGKILL",
    });
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ETIMEDOUT")
      throw new Error(
        "Timed out waiting for Stripe forwarding. The daemon may still be starting; run mise run stripe:status for diagnostics, then retry mise run stripe:listen.",
      );
    throw new Error(
      "Cannot start Stripe forwarding. Check Pitchfork and server readiness, then rerun mise run stripe:listen; use mise run stripe:status for diagnostics.",
    );
  }
  console.log(
    "Stripe forwarding started/restarted. This does not reload server/worker; reload them after saving billing configuration. Run mise run stripe:status to check forwarding.",
  );
} catch (error) {
  console.error(
    error instanceof Error ? error.message : "Stripe listener failed.",
  );
  process.exitCode = 1;
}
