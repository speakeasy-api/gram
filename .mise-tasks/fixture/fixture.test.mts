import assert from "node:assert/strict";
import { execFileSync, spawn, spawnSync } from "node:child_process";
import { once } from "node:events";
import {
  chmodSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  statSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { createServer } from "node:net";
import { join } from "node:path";
import { test, type TestContext } from "node:test";
import { fileURLToPath } from "node:url";
import { cleanupScenarios, validateTLS } from "./lib.ts";
import { connectMCP } from "./smoke.mts";

const runTask = fileURLToPath(new URL("./run.mts", import.meta.url));
const envTask = fileURLToPath(new URL("./env.mts", import.meta.url));
const scratch = fileURLToPath(new URL("../../.local/", import.meta.url));

function fixture(t: TestContext) {
  mkdirSync(scratch, { recursive: true });
  const root = mkdtempSync(join(scratch, "fixture-review-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const key = join(root, "key.pem");
  const cert = join(root, "cert.pem");
  execFileSync(
    "openssl",
    [
      "req",
      "-x509",
      "-newkey",
      "rsa:2048",
      "-nodes",
      "-keyout",
      key,
      "-out",
      cert,
      "-days",
      "1",
      "-subj",
      "/CN=fixture-smoke",
      "-addext",
      "subjectAltName=IP:127.0.0.1",
    ],
    { stdio: "ignore" },
  );
  chmodSync(key, 0o600);
  const config = {
    FIXTURE_TLS_CERT_FILE: cert,
    FIXTURE_TLS_KEY_FILE: key,
    FIXTURE_CA_FILE: cert,
    FIXTURE_CONTROL_SECRET: "e".repeat(64),
    FIXTURE_BASE_URL: "https://127.0.0.1:18750",
    FIXTURE_SMOKE_ORIGIN: "https://127.0.0.1:18751",
    FIXTURE_PORT: "18750",
    FIXTURE_CLIENT_PORT: "18751",
    FIXTURE_CLIENT_ORIGINS: "https://127.0.0.1:18751",
    FIXTURE_BIN: "benchmark-mcp-server",
  };
  const bin = join(root, "bin");
  mkdirSync(bin);
  const calls = join(root, "calls.jsonl");
  const executable = join(bin, "fixture");
  writeFileSync(
    executable,
    `#!${process.execPath}
import fs from 'node:fs';
if (process.env.FAKE_MODE === 'crash') process.kill(process.pid, 'SIGTERM');
else if (process.env.FAKE_MODE === 'wait') {
  console.log('fixture-started');
  setInterval(() => {}, 1000);
} else fs.appendFileSync(process.env.CALLS, JSON.stringify({
  phase: 'fixture',
  privateCA: process.env.SSL_CERT_FILE === process.env.FIXTURE_CA,
  privateSecret: process.env.FIXTURE_CONTROL_SECRET === 'e'.repeat(64)
}) + '\\n');
`,
    { mode: 0o755 },
  );
  writeFileSync(
    join(bin, "mise"),
    `#!${process.execPath}
import fs from 'node:fs';
const args = process.argv.slice(2);
fs.appendFileSync(process.env.CALLS, JSON.stringify({
  phase: args[0],
  pinned: args.includes('go:github.com/speakeasy-api/benchmark-mcp-server/cmd/benchmark-mcp-server@9c6d4f8c2e9828924bbafa7c249ad3eddc234606'),
  privateCA: process.env.SSL_CERT_FILE === process.env.FIXTURE_CA,
  inheritedCA: process.env.SSL_CERT_FILE === 'inherited-trust',
  hasSecret: process.env.FIXTURE_CONTROL_SECRET !== undefined
}) + '\\n');
if (args[0] === 'install') fs.writeFileSync(process.env.INSTALLED, 'installed');
else if (args[0] === 'which' && fs.existsSync(process.env.INSTALLED)) console.log(process.env.FAKE_BINARY);
else process.exit(1);
`,
    { mode: 0o755 },
  );
  writeFileSync(
    join(bin, "git"),
    `#!${process.execPath}\nconsole.log('.git\\n.git');\n`,
    { mode: 0o755 },
  );
  const writeConfig = () => {
    mkdirSync(join(root, ".local"), { recursive: true });
    writeFileSync(
      join(root, ".local/mcp-fixture.env"),
      Object.entries(config)
        .map(([name, value]) => `${name}=${JSON.stringify(value)}`)
        .join("\n") + "\n",
      { mode: 0o600 },
    );
  };
  writeConfig();
  const environment = {
    ...process.env,
    MISE_PROJECT_ROOT: root,
    PATH: `${bin}:${process.env.PATH}`,
    CALLS: calls,
    FAKE_BINARY: executable,
    INSTALLED: join(root, "installed"),
    FIXTURE_CA: cert,
    SSL_CERT_FILE: "inherited-trust",
    FIXTURE_CONTROL_SECRET: "inherited-secret-that-must-not-reach-installation",
  };
  return { root, config, key, executable, calls, environment, writeConfig };
}

test("TLS validates a benign key symlink and rejects target group/other bits without chmod", (t) => {
  const f = fixture(t);
  const link = join(f.root, "key-link.pem");
  symlinkSync(f.key, link);
  const config = { ...f.config, FIXTURE_TLS_KEY_FILE: link };
  validateTLS(config);
  for (const mode of [0o640, 0o604]) {
    chmodSync(f.key, mode);
    assert.throws(
      () => validateTLS(config),
      /no group or other permission bits/,
    );
    assert.equal(statSync(f.key).mode & 0o777, mode);
  }
});

test("cold tool installation resolves the pin before injecting fixture CA or secret", (t) => {
  const f = fixture(t);
  const result = spawnSync(process.execPath, [runTask], {
    env: f.environment,
    encoding: "utf8",
    timeout: 10_000,
  });
  assert.equal(result.status, 0, result.stderr);
  const calls = readFileSync(f.calls, "utf8")
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line));
  assert.deepEqual(
    calls.map((call) => call.phase),
    ["install", "which", "fixture"],
  );
  for (const call of calls.slice(0, 2)) {
    assert.equal(call.pinned, true);
    assert.equal(call.privateCA, false);
    assert.equal(call.inheritedCA, true);
    assert.equal(call.hasSecret, false);
  }
  assert.equal(calls[2].privateCA, true);
  assert.equal(calls[2].privateSecret, true);
  assert.doesNotMatch(
    result.stdout + result.stderr,
    /inherited-secret-that-must-not-reach-installation|e{64}/,
  );
});

test("an independently signalled child is a failure", (t) => {
  const f = fixture(t);
  f.config.FIXTURE_BIN = f.executable;
  f.writeConfig();
  const result = spawnSync(process.execPath, [runTask], {
    env: { ...f.environment, FAKE_MODE: "crash" },
    encoding: "utf8",
    timeout: 10_000,
  });
  assert.equal(result.status, 1, result.stderr);
});

test(
  "a forwarded stop signal exits successfully and stops the owned child",
  { timeout: 10_000 },
  async (t) => {
    const f = fixture(t);
    f.config.FIXTURE_BIN = f.executable;
    f.writeConfig();
    const runner = spawn(process.execPath, [runTask], {
      env: { ...f.environment, FAKE_MODE: "wait" },
      stdio: ["ignore", "pipe", "pipe"],
    });
    t.after(() => runner.kill("SIGTERM"));
    const exited = once(runner, "exit");
    await new Promise<void>((accept, reject) => {
      runner.stdout.on("data", (chunk) => {
        if (chunk.toString().includes("fixture-started")) accept();
      });
      runner.once("exit", () =>
        reject(new Error("owned child exited before readiness")),
      );
    });
    runner.kill("SIGTERM");
    const [code, signal] = await exited;
    assert.equal(code, 0);
    assert.equal(signal, null);
  },
);

test("cleanup attempts every owned scenario and preserves the first deletion failure", async () => {
  const calls: string[] = [];
  const first = new Error("first deletion failed");
  await assert.rejects(
    cleanupScenarios(["zz-first", "zz-second", "zz-third"], async (id) => {
      calls.push(id);
      if (id === "zz-first") throw first;
      if (id === "zz-third") throw new Error("later deletion failed");
    }),
    (error) => error === first,
  );
  assert.deepEqual(calls, ["zz-first", "zz-second", "zz-third"]);
});

test("MCP initialization, notification and calls reuse a stateful session", async () => {
  let initialized = false;
  const calls: string[] = [];
  const connection = await connectMCP(
    async (_url, method, body, headers) => {
      if (method === "DELETE") {
        assert.equal(headers?.["Mcp-Session-Id"], "zz-owned-session");
        calls.push("DELETE");
        return { status: 204, headers: {}, body: "" };
      }
      const rpc = JSON.parse(body!);
      calls.push(rpc.method);
      if (rpc.method === "initialize") {
        assert.equal(headers?.["Mcp-Session-Id"], undefined);
        assert.equal(rpc.params.protocolVersion, "2025-03-26");
        return {
          status: 200,
          headers: { "mcp-session-id": "zz-owned-session" },
          body: JSON.stringify({
            jsonrpc: "2.0",
            id: rpc.id,
            result: {
              protocolVersion: "2025-03-26",
              capabilities: {},
              serverInfo: { name: "synthetic", version: "1" },
            },
          }),
        };
      }
      assert.equal(headers?.["Mcp-Session-Id"], "zz-owned-session");
      if (rpc.method === "notifications/initialized") {
        assert.equal(rpc.id, undefined);
        initialized = true;
        return { status: 202, headers: {}, body: "" };
      }
      assert.equal(
        initialized,
        true,
        "server rejects tools before initialization",
      );
      return {
        status: 200,
        headers: {},
        body: JSON.stringify({
          jsonrpc: "2.0",
          id: rpc.id,
          result: { tools: [{ name: "echo" }] },
        }),
      };
    },
    "https://127.0.0.1/mcp",
    "synthetic-bearer",
  );
  await connection.rpc("tools/list", {});
  await connection.close();
  assert.deepEqual(calls, [
    "initialize",
    "notifications/initialized",
    "tools/list",
    "DELETE",
  ]);
});

test("changing a busy stored binding tells the operator to stop the fixture", async (t) => {
  const f = fixture(t);
  const listener = createServer();
  listener.listen(0, "127.0.0.1");
  await once(listener, "listening");
  t.after(() => listener.close());
  const address = listener.address();
  assert.ok(address && typeof address !== "string");
  f.config.FIXTURE_PORT = String(address.port);
  f.config.FIXTURE_BASE_URL = `https://127.0.0.1:${address.port}`;
  f.writeConfig();
  const result = spawnSync(process.execPath, [envTask], {
    env: {
      ...f.environment,
      GRAM_MCP_FIXTURE: "local",
      usage_port: address.port === 65535 ? "65534" : String(address.port + 1),
    },
    encoding: "utf8",
    timeout: 10_000,
  });
  assert.equal(result.status, 1);
  assert.match(result.stderr, /fixture:stop before changing/);
  assert.doesNotMatch(result.stderr, /select an unused worktree port/);
});
