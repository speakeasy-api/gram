import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
  chmodSync,
  existsSync,
  mkdtempSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { test } from "node:test";

const command = resolve(".mise-tasks/stripe/listen.mts");
const settings =
  '[env]\nSTRIPE_API_KEY="sk_test_fixture"\nSTRIPE_WEBHOOK_SECRET="whsec_fixture"\n';

function fixture() {
  const dir = mkdtempSync(join(tmpdir(), "stripe-public-listen-"));
  mkdirSync(join(dir, "bin"));
  const mock = join(dir, "bin/pitchfork");
  writeFileSync(
    mock,
    `#!/usr/bin/env node
const fs = require('node:fs');
fs.appendFileSync('calls.jsonl', JSON.stringify(process.argv.slice(2)) + '\\n');
if (process.env.FAIL_COMMAND === process.argv[2]) {
  console.error('sk_test_private whsec_private raw child error');
  process.exit(1);
}
`,
  );
  chmodSync(mock, 0o755);
  writeFileSync(join(dir, "mise.local.toml"), settings);
  const run = (env: Record<string, string> = {}) =>
    spawnSync(process.execPath, [command], {
      cwd: dir,
      encoding: "utf8",
      env: {
        ...process.env,
        GRAM_ENVIRONMENT: "local",
        GRAM_SERVER_URL: "http://localhost:18080",
        GRAM_SERVER_PORT: "18080",
        PATH: `${join(dir, "bin")}:${process.env.PATH}`,
        ...env,
      },
    });
  return { dir, run };
}

test("public listen registers internal task before supervisor/start and forces fresh configuration on repeat", () => {
  const { dir, run } = fixture();
  try {
    for (let i = 0; i < 2; i++) {
      const result = run();
      assert.equal(result.status, 0, result.stderr);
    }
    const calls = readFileSync(join(dir, "calls.jsonl"), "utf8")
      .trim()
      .split("\n")
      .map((line) => JSON.parse(line));
    const expected = [
      [
        "daemons",
        "add",
        "stripe-listener",
        "--local",
        "--run",
        "mise run stripe:_listen",
        "--ready-cmd",
        "mise run stripe:status --ready",
        "--depends",
        "server",
      ],
      ["supervisor", "start"],
      ["start", "stripe-listener", "--force"],
    ];
    assert.deepEqual(calls, [...expected, ...expected]);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

for (const mode of [
  "missing-file",
  "missing-key",
  "missing-secret",
  "live-key",
  "remote",
  "port",
  "environment",
] as const) {
  test(`public listen rejects ${mode} before any Pitchfork call`, () => {
    const { dir, run } = fixture();
    try {
      const path = join(dir, "mise.local.toml");
      if (mode === "missing-file") rmSync(path);
      if (mode === "missing-key")
        writeFileSync(path, settings.replace("sk_test_fixture", ""));
      if (mode === "missing-secret")
        writeFileSync(path, settings.replace("whsec_fixture", ""));
      if (mode === "live-key")
        writeFileSync(
          path,
          settings.replace("sk_test_fixture", "sk_live_fixture"),
        );
      const result = run(
        mode === "remote"
          ? { GRAM_SERVER_URL: "https://example.com:18080" }
          : mode === "port"
            ? { GRAM_SERVER_PORT: "18081" }
            : mode === "environment"
              ? { GRAM_ENVIRONMENT: "production" }
              : {},
      );
      assert.equal(result.status, 1);
      assert.equal(existsSync(join(dir, "calls.jsonl")), false);
      assert.equal(existsSync(join(dir, "pitchfork.local.toml")), false);
      if (mode !== "environment")
        assert.match(result.stderr, /mise run stripe:setup/);
      assert.doesNotMatch(result.stderr, /Listener disabled|sk_live_fixture/);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
}

for (const [stage, count] of [
  ["daemons", 1],
  ["supervisor", 2],
  ["start", 3],
] as const) {
  test(`public listen sanitizes ${stage} failures and stops subsequent calls`, () => {
    const { dir, run } = fixture();
    try {
      const result = run({ FAIL_COMMAND: stage });
      assert.equal(result.status, 1);
      assert.doesNotMatch(
        result.stderr + result.stdout,
        /sk_test_|sk_live_|rk_test_|rk_live_|whsec_|raw child error/,
      );
      assert.match(result.stderr, /Cannot (register|start)/);
      assert.equal(
        readFileSync(join(dir, "calls.jsonl"), "utf8").trim().split("\n")
          .length,
        count,
      );
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
}

for (const stage of ["supervisor", "start"] as const) {
  test(`public listen bounds ${stage} wait and sanitizes timeout diagnostics`, () => {
    const { dir, run } = fixture();
    try {
      const preload = join(dir, "timeout.cjs");
      writeFileSync(
        preload,
        `
const cp = require('node:child_process');
const original = cp.execFileSync;
cp.execFileSync = (file, args, options) => {
  if (file === 'pitchfork' && args[0] === '${stage}') {
    require('node:assert/strict').equal(options.timeout, ${stage === "start" ? 120000 : 30000});
    require('node:assert/strict').equal(options.killSignal, 'SIGKILL');
    const error = new Error('sk_test_fixture whsec_fixture');
    error.code = 'ETIMEDOUT';
    throw error;
  }
  return original(file, args, options);
};
require('node:module').syncBuiltinESMExports();
`,
      );
      const result = run({ NODE_OPTIONS: `--require=${preload}` });
      assert.equal(result.status, 1);
      assert.match(result.stderr, /Timed out waiting for Stripe forwarding/);
      assert.match(result.stderr, /mise run stripe:status/);
      assert.doesNotMatch(result.stderr + result.stdout, /sk_test_|whsec_/);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
}
