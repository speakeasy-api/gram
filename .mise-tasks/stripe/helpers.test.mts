import assert from "node:assert/strict";
import { test } from "node:test";
import {
  mkdtempSync,
  writeFileSync,
  mkdirSync,
  readFileSync,
  rmSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { execFileSync, spawn, spawnSync } from "node:child_process";
import { setTimeout as delay } from "node:timers/promises";
import {
  classifyStripeOutput,
  evaluateStripeReadiness,
  localWebhookTarget,
  persistedStripeConfig,
  registerStripeListener,
  stripeConfigIssue,
  stripeFingerprint,
} from "./helpers.mts";
import type { ListenerState, StripeConfig } from "./helpers.mts";

const config: StripeConfig = {
  enabled: true,
  key: "sk_test_fixture",
  secret: "whsec_fixture",
  target: "http://localhost:18080/rpc/stripe.webhook",
};
const root = resolve(".");

for (const environment of ["production", "staging", ""]) {
  test(`listener rejects non-local environment: ${environment || "unset"}`, () => {
    const result = spawnSync(
      process.execPath,
      [
        "--experimental-strip-types",
        join(root, ".mise-tasks/stripe/listen.mts"),
      ],
      {
        env: { ...process.env, GRAM_ENVIRONMENT: environment },
        encoding: "utf8",
      },
    );
    assert.equal(result.status, 1);
    assert.match(
      result.stderr,
      /Stripe forwarding requires GRAM_ENVIRONMENT=local/,
    );
  });
}

test("forwarding is restricted to the worktree loopback port", () => {
  assert.equal(
    localWebhookTarget("http://localhost:18080", "18080"),
    config.target,
  );
  assert.equal(
    localWebhookTarget("https://[::1]:18443", "18443"),
    "https://[::1]:18443/rpc/stripe.webhook",
  );
  for (const url of [
    "https://example.com:18080",
    "http://localhost:18081",
    "http://localhost:18080/other",
    "http://user@localhost:18080",
    "http://localhost:18080?x=y",
    "ftp://localhost:18080",
    "http://localhost.evil.test:18080",
  ])
    assert.throws(() => localWebhookTarget(url, "18080"));
});

test("configuration requires explicit opt-in, a test key, and signing secret", () => {
  assert.equal(stripeConfigIssue(config), undefined);
  for (const key of ["", "unset", "sk_live_fixture", "rk_live_fixture"])
    assert.ok(stripeConfigIssue({ ...config, key }));
  assert.ok(stripeConfigIssue({ ...config, enabled: false }));
  assert.ok(stripeConfigIssue({ ...config, secret: "" }));
});

test("connection classification matches pinned CLI debug records, not payloads", () => {
  for (const message of [
    "Disconnected from Stripe",
    "Resetting the connection",
    "Attempting to connect to Stripe",
    "Failed to connect to Stripe. Retrying...",
  ]) {
    assert.equal(
      classifyStripeOutput(
        `time="fixture" level=debug msg="${message}" prefix=websocket.Client.Run`,
        config.secret,
      ),
      "disconnected",
    );
  }
  const connected =
    'time="fixture" level=debug msg="Connected!" prefix=websocket.Client.connect';
  assert.equal(classifyStripeOutput(connected, config.secret), "connected");
  assert.equal(
    classifyStripeOutput(`payload ${connected}`, config.secret),
    undefined,
  );
});

// v0.5.2 of the CLI's prefixed formatter wraps strings without escaping their
// quotes. Keep nested JSON quotes literal, rather than JSON-encoding logfmt.
for (const [name, metadata] of Object.entries({
  expiry: "expired_api_key",
  authentication: "invalid_api_key",
  secret: "whsec_other",
  ready: "Ready! Your webhook signing secret is whsec_fixture",
  delivery: "[200] POST /rpc/stripe.webhook",
  failure: "[500] POST /rpc/stripe.webhook connection refused",
  connected: 'msg="Connected!" prefix=websocket.Client.connect',
  disconnected: 'msg="Disconnected from Stripe" prefix=websocket.Client.Run',
  fatal: 'level=fatal msg="expired_api_key"',
  wholeRecord:
    'time="fixture" level=debug msg="Connected!" prefix=websocket.Client.connect',
})) {
  test(`structured payload isolates ${name} metadata`, () => {
    const payload = JSON.stringify({
      data: { object: { metadata: { note: metadata } } },
    });
    for (const level of ["debug", "error"]) {
      const record = `time="fixture" level=${level} msg="Incoming message" message="${payload}" prefix=websocket.Client.readPump`;
      assert.equal(classifyStripeOutput(record, config.secret), undefined);
    }
  });
}

test("only complete connection records are trusted, not extra fields or messages", () => {
  for (const record of [
    'time="fixture" level=debug msg="Connected!" message="expired_api_key" prefix=websocket.Client.connect',
    'time="fixture" level=debug msg="Disconnected from Stripe" message="whsec_other" prefix=websocket.Client.Run',
    'time="fixture" level=debug msg="expired_api_key" prefix=proxy.Proxy.GetSessionSecret',
    'time="fixture" level=error msg="[200] POST /rpc/stripe.webhook whsec_other"',
    'time="fixture" level=fatal msg="Ready! Your webhook signing secret is whsec_other [200] POST /rpc/stripe.webhook"',
  ])
    assert.equal(classifyStripeOutput(record, config.secret), undefined);
});

test("authentic structured fatal authentication failures remain actionable", () => {
  for (const [body, phase] of [
    ['{"error":{"code":"expired_api_key"}}', "expired"],
    ['{"error":{"message":"Invalid API Key provided"}}', "auth-failed"],
    ["{}", "auth-failed"],
  ] as const) {
    for (const prefix of ["", "Error while authenticating with Stripe: "]) {
      const record = `time="fixture" level=fatal msg="${prefix}Authorization failed, status=401, body=${body}"`;
      assert.equal(classifyStripeOutput(record, config.secret), phase);
      // Preflight supplies stdout + newline + stderr, including leading blanks.
      assert.equal(classifyStripeOutput(`\n${record}\n`, config.secret), phase);
    }
  }
});

test("pinned CLI timestamped human output retains ready and HTTP signals", () => {
  for (const [line, phase] of [
    [
      "> Ready! You are using Stripe API Version [fixture]. Your webhook signing secret is whsec_fixture (^C to quit)",
      "ready",
    ],
    [
      "2026-01-01 10:00:00  <--  [200] POST http://localhost:18080/rpc/stripe.webhook [evt_fixture]",
      "delivered",
    ],
    [
      "2026-01-01 10:00:00  <--  [400] POST http://localhost:18080/rpc/stripe.webhook [evt_fixture]",
      "delivery-failed",
    ],
    [
      "2026-01-01 10:00:00            [ERROR] Failed to POST: connection refused",
      "delivery-failed",
    ],
  ] as const)
    assert.equal(classifyStripeOutput(line, config.secret), phase);
});

test("CLI output classification never confuses ready with delivery", () => {
  assert.equal(
    classifyStripeOutput(
      "Ready! Your webhook signing secret is whsec_fixture",
      config.secret,
    ),
    "ready",
  );
  assert.equal(
    classifyStripeOutput(
      "Ready! Your webhook signing secret is whsec_other",
      config.secret,
    ),
    "secret-mismatch",
  );
  assert.equal(
    classifyStripeOutput(
      "<-- [200] POST http://localhost:18080/rpc/stripe.webhook [evt_fixture]",
      config.secret,
    ),
    "delivered",
  );
  assert.equal(
    classifyStripeOutput(
      "<-- [400] POST http://localhost:18080/rpc/stripe.webhook",
      config.secret,
    ),
    "delivery-failed",
  );
  assert.equal(
    classifyStripeOutput("expired_api_key", config.secret),
    "expired",
  );
  assert.equal(
    classifyStripeOutput("Invalid API Key provided", config.secret),
    "auth-failed",
  );
});

test("status rejects stale sessions and preserves actionable expiry after exit", () => {
  const state: ListenerState = {
    pid: 123,
    phase: "ready",
    fingerprint: stripeFingerprint(config),
    updatedAt: new Date().toISOString(),
  };
  assert.deepEqual(
    [
      evaluateStripeReadiness(config, state, true).ready,
      evaluateStripeReadiness(config, state, true).deliveryVerified,
    ],
    [true, false],
  );
  assert.equal(
    evaluateStripeReadiness(config, { ...state, phase: "delivered" }, true)
      .deliveryVerified,
    true,
  );
  assert.equal(evaluateStripeReadiness(config, state, false).ready, false);
  assert.equal(
    evaluateStripeReadiness({ ...config, key: "sk_test_changed" }, state, true)
      .ready,
    false,
  );
  assert.equal(
    evaluateStripeReadiness(config, { ...state, phase: "expired" }, false).code,
    "key-expired",
  );
});

function fixture() {
  const dir = mkdtempSync(join(tmpdir(), "gram-stripe-test-"));
  mkdirSync(join(dir, "bin"));
  writeFileSync(
    join(dir, "mise.local.toml"),
    '[env]\nSTRIPE_API_KEY = "sk_test_fixture"\nSTRIPE_WEBHOOK_SECRET = "whsec_fixture"\n',
  );
  writeFileSync(
    join(dir, "pitchfork.local.toml"),
    '[daemons.stripe-listener]\nrun="mise run stripe:listen"\n',
  );
  execFileSync("git", ["init", "-q", dir], { stdio: "ignore" });
  return dir;
}

test("persisted credentials win over ambient CLI credentials", () => {
  const dir = fixture();
  try {
    const result = persistedStripeConfig(dir, {
      STRIPE_API_KEY: "sk_live_ambient",
      GRAM_SERVER_URL: "http://localhost:18080",
      GRAM_SERVER_PORT: "18080",
    });
    assert.equal(result.key, config.key);
    assert.equal(result.enabled, true);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("native local registration is opt-in, repeatable, and removable", () => {
  const dir = fixture();
  const local = join(dir, "pitchfork.local.toml");
  const env = {
    GRAM_SERVER_URL: "http://localhost:8080",
    GRAM_SERVER_PORT: "8080",
  };
  try {
    writeFileSync(
      join(dir, "pitchfork.toml"),
      '[daemons.server]\nrun="true"\n',
    );
    writeFileSync(local, '[daemons.other]\nrun="true"\n');
    assert.equal(persistedStripeConfig(dir, env).enabled, false);
    registerStripeListener(dir);
    const registered = readFileSync(local, "utf8");
    assert.match(registered, /\[daemons\.stripe-listener\]/);
    assert.match(registered, /ready_cmd = "mise run stripe:status --ready"/);
    assert.match(registered, /depends = \["server"\]/);
    assert.match(registered, /\[daemons\.other\]/);
    assert.equal(persistedStripeConfig(dir, env).enabled, true);
    registerStripeListener(dir);
    assert.equal(readFileSync(local, "utf8"), registered);
    assert.equal(
      readFileSync(join(dir, "pitchfork.toml"), "utf8"),
      '[daemons.server]\nrun="true"\n',
    );
    execFileSync(
      "pitchfork",
      ["daemons", "remove", "stripe-listener", "--local"],
      { cwd: dir, stdio: "pipe" },
    );
    assert.equal(persistedStripeConfig(dir, env).enabled, false);
    assert.match(readFileSync(local, "utf8"), /\[daemons\.other\]/);
    rmSync(local);
    assert.equal(persistedStripeConfig(dir, env).enabled, false);
    writeFileSync(local, "invalid [");
    assert.throws(() => registerStripeListener(dir), /Cannot register/);
    assert.equal(readFileSync(local, "utf8"), "invalid [");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

for (const [mode, expected] of [
  ["mismatch", "secret-mismatch"],
  ["expired", "expired"],
] as const) {
  test(`listener refuses to forward on ${mode}`, async () => {
    const dir = fixture();
    writeFileSync(
      join(dir, "bin/stripe"),
      `#!/usr/bin/env node
if (!process.argv.includes('--print-secret')) { require('fs').writeFileSync('unexpected-forwarding', 'yes'); process.exit(9); }
${mode === "mismatch" ? "console.log('whsec_other');" : "console.error('expired_api_key sk_test_fixture'); process.exit(1);"}
`,
      { mode: 0o755 },
    );
    const child = spawn(
      process.execPath,
      [
        "--experimental-strip-types",
        join(root, ".mise-tasks/stripe/listen.mts"),
      ],
      {
        cwd: dir,
        env: {
          ...process.env,
          GRAM_ENVIRONMENT: "local",
          PATH: `${join(dir, "bin")}:${process.env.PATH}`,
          GRAM_SERVER_URL: "http://localhost:18080",
          GRAM_SERVER_PORT: "18080",
        },
        stdio: ["ignore", "pipe", "pipe"],
      },
    );
    let output = "";
    child.stdout.on("data", (chunk) => {
      output += String(chunk);
    });
    child.stderr.on("data", (chunk) => {
      output += String(chunk);
    });
    try {
      const code = await new Promise((resolve) => child.on("exit", resolve));
      assert.equal(code, 1);
      assert.equal(
        JSON.parse(
          readFileSync(join(dir, ".git/gram-stripe-listener.json"), "utf8"),
        ).phase,
        expected,
      );
      assert.throws(() => readFileSync(join(dir, "unexpected-forwarding")));
      assert.doesNotMatch(output, /whsec_|sk_test_/);
    } finally {
      child.kill();
      rmSync(dir, { recursive: true, force: true });
    }
  });
}

test("supervised listener tracks disconnect/reconnect without leaking raw output", async () => {
  const dir = fixture();
  const script = `#!/usr/bin/env node
const fs = require('node:fs');
if (process.env.STRIPE_API_KEY !== 'sk_test_fixture') process.exit(8);
if (process.argv.includes('--print-secret')) { console.log('whsec_fixture'); process.exit(0); }
if (!process.argv.includes('--log-level') || !process.argv.includes('debug')) process.exit(9);
console.error('time="fixture" level=debug msg="Disconnected from Stripe" prefix=websocket.Client.Run');
console.error('time="fixture" level=debug msg="Connected!" prefix=websocket.Client.connect');
console.error('<-- [200] POST http://localhost:18080/rpc/stripe.webhook [evt_fixture]');
let disconnected = false;
let reconnected = false;
setInterval(() => {
  if (!disconnected && fs.existsSync('disconnect')) {
    disconnected = true;
    console.error('time="fixture" level=debug msg="Disconnected from Stripe" prefix=websocket.Client.Run');
    console.error('time="fixture" level=debug msg="Failed to connect to Stripe. Retrying..." prefix=websocket.Client.Run');
    // Queued banner/HTTP output must not restore a disconnected socket.
    console.error('Ready! Your webhook signing secret is whsec_fixture');
    console.error('<-- [200] POST http://localhost:18080/rpc/stripe.webhook [evt_fixture]');
    console.error('private debug payload sk_test_fixture whsec_fixture evt_fixture');
    console.error('time="fixture" level=debug msg="Incoming message" message="' + JSON.stringify({data: {object: {metadata: {note: 'expired_api_key whsec_other [200] POST /rpc/stripe.webhook msg="Connected!" prefix=websocket.Client.connect'}}}}) + '" prefix=websocket.Client.readPump');
  }
  if (disconnected && !reconnected && fs.existsSync('reconnect')) {
    reconnected = true;
    console.error('time="fixture" level=debug msg="Connected!" prefix=websocket.Client.connect');
  }
}, 20);
`;
  writeFileSync(join(dir, "bin/stripe"), script, { mode: 0o755 });
  const child = spawn(
    process.execPath,
    ["--experimental-strip-types", join(root, ".mise-tasks/stripe/listen.mts")],
    {
      cwd: dir,
      env: {
        ...process.env,
        GRAM_ENVIRONMENT: "local",
        PATH: `${join(dir, "bin")}:${process.env.PATH}`,
        STRIPE_API_KEY: "sk_live_ambient",
        GRAM_SERVER_URL: "http://localhost:18080",
        GRAM_SERVER_PORT: "18080",
      },
      stdio: ["ignore", "pipe", "pipe"],
    },
  );
  let output = "";
  child.stdout.on("data", (chunk) => {
    output += String(chunk);
  });
  child.stderr.on("data", (chunk) => {
    output += String(chunk);
  });
  const exited = new Promise((resolve) => child.on("exit", resolve));
  try {
    let state: ListenerState | undefined;
    for (let i = 0; i < 1000; i++) {
      try {
        state = JSON.parse(
          readFileSync(join(dir, ".git/gram-stripe-listener.json"), "utf8"),
        );
      } catch {
        /* starting */
      }
      if (state?.phase === "delivered") break;
      await delay(30);
    }
    assert.equal(state?.phase, "delivered", output);
    assert.equal(
      evaluateStripeReadiness(config, state, true).deliveryVerified,
      true,
    );
    for (const [command, phase] of [
      ["disconnect", "disconnected"],
      ["reconnect", "ready"],
    ] as const) {
      writeFileSync(join(dir, command), "");
      for (let i = 0; i < 200; i++) {
        state = JSON.parse(
          readFileSync(join(dir, ".git/gram-stripe-listener.json"), "utf8"),
        );
        if (state?.phase === phase) break;
        await delay(30);
      }
      assert.equal(state?.phase, phase, output);
      const readiness = evaluateStripeReadiness(config, state, true);
      assert.equal(readiness.ready, phase === "ready");
      assert.equal(readiness.deliveryVerified, false);
      assert.doesNotMatch(JSON.stringify(state), /whsec_|sk_test_|evt_fixture/);
    }
    assert.doesNotMatch(output, /whsec_|sk_test_|sk_live_|evt_fixture/);
  } finally {
    child.kill("SIGTERM");
    assert.equal(await exited, 0, "intentional stop must exit successfully");
    rmSync(dir, { recursive: true, force: true });
  }
});
