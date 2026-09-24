import assert from "node:assert/strict";
import { test } from "node:test";
import { execFileSync } from "node:child_process";
import {
  mkdtempSync,
  mkdirSync,
  writeFileSync,
  realpathSync,
  rmSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { getSetupReadiness } from "./readiness.mts";
import {
  persistedStripeConfig,
  listenerProcessStarted,
  stripeFingerprint,
  stripeStatePath,
  writeStripeState,
} from "./helpers.mts";

test("readiness needs no organization, CSV or database and distinguishes delivery", async (t) => {
  const root = realpathSync(mkdtempSync(join(tmpdir(), "gram-stripe-status-")));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "bin"));
  writeFileSync(
    join(root, "bin", "stripe"),
    '#!/bin/sh\n[ "$1" = version ] || exit 99\n',
    { mode: 0o700 },
  );
  execFileSync("git", ["init", "--quiet", root]);
  const oldPath = process.env.PATH;
  process.env.PATH = `${join(root, "bin")}:${oldPath}`;
  t.after(() => {
    process.env.PATH = oldPath;
  });
  writeFileSync(
    join(root, "pitchfork.local.toml"),
    '[daemons.stripe-listener]\nrun="mise run stripe:listen"\n',
  );
  const settings = {
    STRIPE_API_KEY: "sk_test_synthetic",
    STRIPE_WEBHOOK_SECRET: "whsec_synthetic",
    STRIPE_PRICE_ID_TUM: "price_synthetic",
    STRIPE_PRICE_ID_MCP_EGRESS: "price_mcp_egress",
    STRIPE_PRICE_ID_RISK_SCANS: "price_risk_scans",
    STRIPE_METER_ID_TUM: "mtr_synthetic",
    STRIPE_PORTAL_CONFIGURATION_ID: "bpc_synthetic",
    STRIPE_METER_EVENT_NAME: "tum",
  };
  writeFileSync(
    join(root, "mise.local.toml"),
    "[env]\n" +
      Object.entries(settings)
        .map(([key, value]) => `${key} = ${JSON.stringify(value)}`)
        .join("\n"),
  );
  const env = {
    GRAM_ENVIRONMENT: "local",
    GRAM_SERVER_URL: "http://localhost:8080",
    GRAM_SERVER_PORT: "8080",
    GRAM_CONTROL_PORT: "8081",
  };
  const requests = t.mock.method(globalThis, "fetch", async (url: string) => {
    assert.equal(url, "http://127.0.0.1:8081/healthz");
    return new Response("ok");
  });
  const config = persistedStripeConfig(root, env);
  const state = {
    pid: process.pid,
    processStarted: listenerProcessStarted(process.pid),
    fingerprint: stripeFingerprint(config),
    phase: "ready" as const,
    updatedAt: new Date().toISOString(),
  };
  writeStripeState(stripeStatePath(root), state);
  const ready = await getSetupReadiness(root, env);
  assert.equal(ready.configurationReady, true);
  assert.equal(ready.deliveryVerified, false);
  assert.equal(ready.serverConfigurationVerified, false);
  for (const value of [settings.STRIPE_API_KEY, settings.STRIPE_WEBHOOK_SECRET])
    assert.ok(!JSON.stringify(ready).includes(value));
  writeStripeState(stripeStatePath(root), { ...state, phase: "delivered" });
  assert.equal((await getSetupReadiness(root, env)).deliveryVerified, true);
  const stale = await getSetupReadiness(root, {
    ...env,
    STRIPE_API_KEY: "sk_test_stale",
  });
  assert.equal(stale.configurationReady, false);
  assert.equal(
    stale.checks.find((check) => check.code === "environment")?.ok,
    false,
  );
  const calls = requests.mock.callCount();
  const remote = await getSetupReadiness(root, {
    ...env,
    GRAM_SERVER_URL: "https://example.invalid",
  });
  assert.equal(remote.configurationReady, false);
  assert.equal(requests.mock.callCount(), calls);
  writeStripeState(stripeStatePath(root), {
    ...state,
    processStarted: "old process start",
    phase: "delivered",
  });
  const recycled = await getSetupReadiness(root, env);
  assert.equal(recycled.listenerReady, false);
  assert.equal(recycled.deliveryVerified, false);
  writeFileSync(join(root, "bin", "stripe"), "#!/bin/sh\nexit 1\n", {
    mode: 0o700,
  });
  assert.equal(
    (await getSetupReadiness(root, env)).checks.find(
      (check) => check.code === "cli",
    )?.ok,
    false,
  );
});
