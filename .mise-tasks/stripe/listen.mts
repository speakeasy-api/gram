#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Run the opt-in worktree Stripe sandbox webhook listener"
//MISE hide=true
//MISE quiet=true

import { execFileSync, spawn } from "node:child_process";
import { createInterface } from "node:readline";
import {
  classifyStripeOutput,
  listenerProcessStarted,
  persistedStripeConfig,
  stripeConfigIssue,
  stripeFingerprint,
  stripeStatePath,
  writeStripeState,
} from "./helpers.mts";
import type { ListenerPhase } from "./helpers.mts";

try {
  const config = persistedStripeConfig();
  const issue = stripeConfigIssue(config);
  if (issue) throw new Error(issue);
  const path = stripeStatePath();
  const fingerprint = stripeFingerprint(config);
  const processStarted = listenerProcessStarted(process.pid);
  if (!processStarted)
    throw new Error(
      "Cannot establish listener process identity; check local ps availability.",
    );
  let phase: ListenerPhase = "starting";
  const save = (next: ListenerPhase) => {
    phase = next;
    writeStripeState(path, {
      pid: process.pid,
      processStarted,
      fingerprint,
      phase,
      updatedAt: new Date().toISOString(),
    });
  };
  save("starting");
  // Explicit API-key environment overrides the CLI login. Never put a key in argv.
  const env = { ...process.env, STRIPE_API_KEY: config.key, NO_COLOR: "1" };
  let preflight = "";
  try {
    preflight = execFileSync(
      "stripe",
      ["listen", "--latest", "--print-secret", "--skip-update"],
      {
        env,
        encoding: "utf8",
        timeout: 30_000,
        maxBuffer: 64 * 1024,
        stdio: ["ignore", "pipe", "pipe"],
      },
    );
  } catch (error) {
    const output = error as {
      stdout?: string | Buffer;
      stderr?: string | Buffer;
    };
    const diagnosis = classifyStripeOutput(
      `${output.stdout ?? ""}\n${output.stderr ?? ""}`,
      config.secret,
    );
    save(
      diagnosis === "expired" || diagnosis === "auth-failed"
        ? diagnosis
        : "stopped",
    );
    throw new Error(
      "Stripe listener preflight failed. Check Stripe CLI installation and run mise run stripe:status; refresh the sandbox API key with stripe:setup if needed.",
    );
  }
  if (preflight.match(/whsec_[A-Za-z0-9]+/)?.[0] !== config.secret) {
    save("secret-mismatch");
    throw new Error(
      "Stripe signing secret mismatch. Run mise run stripe:setup and restart server and stripe-listener.",
    );
  }
  const child = spawn(
    "stripe",
    [
      "listen",
      "--latest",
      "--skip-update",
      "--skip-verify",
      "--color",
      "off",
      "--log-level",
      "debug",
      "--forward-to",
      config.target,
    ],
    { env, stdio: ["ignore", "pipe", "pipe"] },
  );
  // Raw CLI output includes the signing secret and must never reach Pitchfork logs.
  let verified = false;
  let connected = false;
  const deadline = setTimeout(() => {
    save("stopped");
    child.kill("SIGTERM");
  }, 45_000);
  const consume = (line: string) => {
    const next = classifyStripeOutput(line, config.secret);
    if (!next) return;
    if (
      next === "secret-mismatch" ||
      next === "expired" ||
      next === "auth-failed"
    ) {
      save(next);
      clearTimeout(deadline);
      child.kill("SIGTERM");
      return;
    }
    if (next === "disconnected") {
      connected = false;
      save(next);
      console.log(
        "Stripe listener disconnected or reconnecting; delivery unverified.",
      );
      return;
    }
    // The Ready banner verifies the secret once; reconnects only emit Connected!.
    if (next === "ready") verified = true;
    if (next === "connected") connected = true;
    if (next === "ready" || next === "connected") {
      if (!verified || !connected) return;
      clearTimeout(deadline);
      save("ready");
      console.log(
        "Stripe listener ready (signing secret matched; delivery unverified).",
      );
    } else if (verified && phase !== "disconnected") {
      save(next);
      console.log(
        next === "delivered"
          ? "Stripe webhook received a successful HTTP response."
          : "Stripe webhook forwarding failed; run mise run stripe:status.",
      );
    }
  };
  createInterface({ input: child.stdout }).on("line", consume);
  createInterface({ input: child.stderr }).on("line", consume);
  for (const signal of ["SIGINT", "SIGTERM"] as const)
    process.on(signal, () => {
      child.kill(signal);
    });
  child.on("error", () => {
    clearTimeout(deadline);
    save("stopped");
    console.error("Unable to start Stripe CLI. Install it and retry.");
    process.exitCode = 1;
  });
  child.on("exit", (code) => {
    clearTimeout(deadline);
    if (!["expired", "auth-failed", "secret-mismatch"].includes(phase))
      save("stopped");
    process.exitCode = code ?? 1;
  });
} catch (error) {
  // Only our own fixed messages are safe; unexpected process errors may contain secrets.
  console.error(
    error instanceof Error && !("cmd" in error) && !("stdout" in error)
      ? error.message
      : "Stripe listener failed. Run mise run stripe:status.",
  );
  process.exitCode = 1;
}
