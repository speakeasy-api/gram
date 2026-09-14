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
import { execFileSync, spawn } from "node:child_process";
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

test("supervised listener uses persisted key and never leaks raw CLI output", async () => {
  const dir = fixture();
  const script = `#!/usr/bin/env node
if (process.env.STRIPE_API_KEY !== 'sk_test_fixture') process.exit(8);
if (process.argv.includes('--print-secret')) { console.log('whsec_fixture'); process.exit(0); }
console.log('Ready! Your webhook signing secret is whsec_fixture');
console.log('<-- [200] POST http://localhost:18080/rpc/stripe.webhook [evt_fixture]');
setInterval(() => {}, 1000);
`;
  writeFileSync(join(dir, "bin/stripe"), script, { mode: 0o755 });
  const child = spawn(
    process.execPath,
    ["--experimental-strip-types", join(root, ".mise-tasks/stripe/listen.mts")],
    {
      cwd: dir,
      env: {
        ...process.env,
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
    assert.doesNotMatch(output, /whsec_|sk_test_|sk_live_|evt_fixture/);
  } finally {
    child.kill("SIGTERM");
    await exited;
    rmSync(dir, { recursive: true, force: true });
  }
});
