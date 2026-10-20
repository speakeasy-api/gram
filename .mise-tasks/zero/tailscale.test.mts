import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import {
  existsSync,
  chmodSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  symlinkSync,
  statSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test, type TestContext } from "node:test";
import { fileURLToPath } from "node:url";
import { parseTOML } from "confbox";
import { dependentEnvVars, setupTailscale } from "./tailscale.mts";

const hostname = "dev-machine.example.ts.net";
const ipv4 = "100.64.0.1";
const ipv6 = "fd7a:115c:a1e0::1";
const status = {
  BackendState: "Running",
  Self: { DNSName: `${hostname}.`, TailscaleIPs: [ipv4, ipv6] },
};
const source = parseTOML(
  readFileSync(new URL("../../mise.toml", import.meta.url), "utf8"),
) as { env: Record<string, unknown> };
const dependents = dependentEnvVars(source.env, ["GRAM_HOST", "GRAM_BIND_IP"]);
const bindings: [string, string][] = [
  ["GRAM_DEVIDP_ADDRESS", source.env.GRAM_DEVIDP_ADDRESS as string],
  [
    "GRAM_DEVIDP_CONTROL_ADDRESS",
    source.env.GRAM_DEVIDP_CONTROL_ADDRESS as string,
  ],
];
const keys = new Set(["GRAM_HOST", "GRAM_BIND_IP"]);
for (const [key, value] of [...dependents, ...bindings]) {
  keys.add(key);
  for (const match of value.matchAll(/\benv\.([A-Za-z_][A-Za-z0-9_]*)\b/g)) {
    keys.add(match[1]);
  }
}
const config = Object.fromEntries(
  Object.entries(source.env).filter(([key]) => keys.has(key)),
);
const mise = execFileSync("sh", ["-c", "command -v mise"], {
  encoding: "utf8",
}).trim();

function fixture(t: TestContext, withCLI = true) {
  const root = mkdtempSync(join(tmpdir(), "tailscale-setup-test-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const bin = join(root, "bin");
  mkdirSync(bin);
  symlinkSync(mise, join(bin, "mise"));
  const calls = join(root, "tailscale-calls.jsonl");
  if (withCLI) {
    writeFileSync(
      join(bin, "tailscale"),
      `#!${process.execPath}
const fs = require('node:fs');
fs.appendFileSync(process.env.TEST_CALLS, JSON.stringify({ args: process.argv.slice(2), cli: process.env.TAILSCALE_BE_CLI }) + '\\n');
process.stdout.write(process.env.TEST_STATUS);
process.exit(Number(process.env.TEST_EXIT || 0));
`,
      { mode: 0o755 },
    );
  }
  const local = {
    UNRELATED: "keep",
    GRAM_SERVER_PORT: "23456",
    GRAM_SITE_PORT: "23457",
    GRAM_ADMIN_PORT: "23458",
    GRAM_ADMIN_DASHBOARD_PORT: "23459",
    ...Object.fromEntries(bindings),
    ...Object.fromEntries([...dependents].reverse()),
    GRAM_HOST: "localhost",
    GRAM_BIND_IP: "127.0.0.1",
    GRAM_SSL_EXTRA_HOSTS: "existing.example.com",
  };
  const writeConfig = (file: string, values: Record<string, unknown>) =>
    writeFileSync(
      join(root, file),
      "[env]\n" +
        Object.entries(values)
          .map(([key, value]) => `${key} = ${JSON.stringify(value)}`)
          .join("\n") +
        "\n",
    );
  writeConfig("mise.toml", config);
  writeConfig("mise.local.toml", local);
  const env: NodeJS.ProcessEnv = {
    PATH: bin,
    HOME: root,
    MISE_CONFIG_DIR: join(root, "mise-config"),
    MISE_TRUSTED_CONFIG_PATHS: root,
    TEST_CALLS: calls,
    TEST_STATUS: JSON.stringify(status),
  };
  const messages: string[] = [];
  t.mock.method(console, "log", (message: string) => messages.push(message));
  const raw = () => readFileSync(join(root, "mise.local.toml"), "utf8");
  const read = () => (parseTOML(raw()) as { env: Record<string, string> }).env;
  return { root, env, local, calls, messages, raw, read, writeConfig };
}

function miseWrapper(root: string, before: string, after = ""): void {
  const path = join(root, "bin", "mise");
  unlinkSync(path);
  writeFileSync(
    path,
    `#!${process.execPath}
const fs = require('node:fs');
const { spawnSync } = require('node:child_process');
const args = process.argv.slice(2);
const file = args[args.indexOf('--file') + 1];
${before}
const result = spawnSync(${JSON.stringify(mise)}, args, { env: process.env, stdio: 'inherit' });
if (result.status !== 0) process.exit(result.status || 1);
${after}
`,
    { mode: 0o755 },
  );
}

for (const skipped of [
  "*",
  "zero:tailscale",
  "zero:github, zero:tailscale",
  "zero:github, *",
]) {
  test(`skip list exits before detection or prompting (${skipped})`, async (t) => {
    const f = fixture(t);
    const before = f.raw();
    await setupTailscale({
      root: f.root,
      env: { ...f.env, GRAM_ZERO_SKIP: skipped },
      prompt: async () => {
        throw new Error("must not prompt");
      },
    });
    assert.equal(existsSync(f.calls), false);
    assert.equal(f.raw(), before);
    assert.match(f.messages.join("\n"), /zero:tailscale --force/);
  });
}

test("missing CLI exits without changing local settings", async (t) => {
  const f = fixture(t, false);
  const before = f.raw();
  await setupTailscale({
    root: f.root,
    env: f.env,
    prompt: async () => {
      throw new Error("must not prompt");
    },
  });
  assert.equal(f.raw(), before);
  assert.match(f.messages.join("\n"), /Install its CLI/);
  assert.match(f.messages.join("\n"), /zero:tailscale --force/);
});

for (const [name, unavailable] of [
  [
    "logged out",
    { TEST_STATUS: JSON.stringify({ ...status, BackendState: "NeedsLogin" }) },
  ],
  [
    "missing DNS",
    {
      TEST_STATUS: JSON.stringify({
        ...status,
        Self: { ...status.Self, DNSName: "" },
      }),
    },
  ],
  [
    "missing IPs",
    {
      TEST_STATUS: JSON.stringify({
        ...status,
        Self: { ...status.Self, TailscaleIPs: [] },
      }),
    },
  ],
  ["invalid JSON", { TEST_STATUS: "not JSON" }],
  ["null status", { TEST_STATUS: "null" }],
  ["daemon error", { TEST_EXIT: "1" }],
] as const) {
  test(`unavailable Tailscale state exits with retry instructions (${name})`, async (t) => {
    const f = fixture(t);
    const before = f.raw();
    await setupTailscale({
      root: f.root,
      env: { ...f.env, ...unavailable },
      prompt: async () => {
        throw new Error("must not prompt");
      },
    });
    assert.equal(f.raw(), before);
    assert.match(f.messages.join("\n"), /zero:tailscale --force/);
  });
}

test("mise maps the skip environment and force flag without configuring a noninteractive shell", (t) => {
  const f = fixture(t);
  symlinkSync(process.execPath, join(f.root, "bin", "node"));
  const tasks = join(f.root, ".mise-tasks", "zero");
  mkdirSync(tasks, { recursive: true });
  symlinkSync(
    fileURLToPath(new URL("./tailscale.mts", import.meta.url)),
    join(tasks, "tailscale.mts"),
  );
  const before = f.raw();
  const run = (args: string[]) =>
    spawnSync("mise", ["run", "zero:tailscale", ...args], {
      cwd: f.root,
      env: { ...f.env, GRAM_ZERO_SKIP: "*" },
      encoding: "utf8",
    });
  const skipped = run([]);
  assert.equal(skipped.status, 0, skipped.stderr);
  assert.match(skipped.stdout, /disabled by GRAM_ZERO_SKIP/);
  assert.equal(existsSync(f.calls), false);
  const forced = run(["--force"]);
  assert.equal(forced.status, 0, forced.stderr);
  assert.match(forced.stdout, /interactive terminal/);
  assert.match(forced.stdout, /zero:tailscale --force/);
  assert.equal(existsSync(f.calls), true);
  assert.equal(f.raw(), before);
});

test("declining merges and persists the skip list without changing other settings", async (t) => {
  const f = fixture(t);
  f.writeConfig("mise.local.toml", {
    ...f.local,
    GRAM_ZERO_SKIP: "zero:idp,zero:github",
  });
  await setupTailscale({
    root: f.root,
    env: { ...f.env, GRAM_ZERO_SKIP: "zero:github, zero:openrouter" },
    prompt: async () => false,
  });
  const updated = f.read();
  assert.equal(
    updated.GRAM_ZERO_SKIP,
    "zero:idp,zero:github,zero:openrouter,zero:tailscale",
  );
  delete updated.GRAM_ZERO_SKIP;
  assert.deepEqual(updated, f.local);
  assert.match(f.messages.join("\n"), /zero:tailscale --force/);
});

test("cancelling does not persist a decline", async (t) => {
  const f = fixture(t);
  const before = f.raw();
  await setupTailscale({
    root: f.root,
    env: f.env,
    prompt: async () => Symbol("cancel"),
  });
  assert.equal(f.raw(), before);
  assert.match(f.messages.join("\n"), /zero:tailscale --force/);
});

test("setup can create mise.local.toml and read addresses from top-level status", async (t) => {
  const f = fixture(t);
  unlinkSync(join(f.root, "mise.local.toml"));
  await setupTailscale({
    root: f.root,
    env: {
      ...f.env,
      TEST_STATUS: JSON.stringify({
        BackendState: "Running",
        Self: { DNSName: `${hostname}.` },
        TailscaleIPs: [ipv4, ipv6],
      }),
    },
    prompt: async () => true,
  });
  const saved = f.read();
  assert.equal(saved.GRAM_HOST, hostname);
  assert.equal(saved.GRAM_BIND_IP, ipv4);
  assert.equal(saved.GRAM_SSL_EXTRA_HOSTS, `${hostname},${ipv4},${ipv6}`);
});

test("setup amends and deduplicates existing local certificate hosts", async (t) => {
  const f = fixture(t);
  f.writeConfig("mise.local.toml", {
    ...f.local,
    GRAM_SSL_EXTRA_HOSTS: ` existing.example.com, ${hostname},${ipv4},existing.example.com, ${ipv6},${ipv4}, `,
  });
  await setupTailscale({
    root: f.root,
    env: { ...f.env, GRAM_SSL_EXTRA_HOSTS: "environment.example.com" },
    prompt: async () => true,
  });
  assert.equal(
    f.read().GRAM_SSL_EXTRA_HOSTS,
    `existing.example.com,${hostname},${ipv4},${ipv6}`,
  );
});

test("completed setup does not prompt again or persist an accidental decline", async (t) => {
  const f = fixture(t);
  await setupTailscale({ root: f.root, env: f.env, prompt: async () => true });
  const before = f.raw();
  const prompt = t.mock.fn(async () => false);
  await setupTailscale({ root: f.root, env: f.env, prompt });
  assert.equal(prompt.mock.callCount(), 0);
  assert.equal(f.raw(), before);
  assert.equal(f.read().GRAM_ZERO_SKIP, undefined);
  assert.match(f.messages.join("\n"), /already configured/);
  assert.match(f.messages.join("\n"), /zero:tailscale --force/);
});

test("force repeats a completed setup", async (t) => {
  const f = fixture(t);
  await setupTailscale({ root: f.root, env: f.env, prompt: async () => true });
  const prompt = t.mock.fn(async () => true);
  await setupTailscale({
    root: f.root,
    env: { ...f.env, usage_force: "true" },
    prompt,
  });
  assert.equal(prompt.mock.callCount(), 1);
});

test("completed setup refreshes newly introduced host dependencies", async (t) => {
  const f = fixture(t);
  await setupTailscale({ root: f.root, env: f.env, prompt: async () => true });
  f.writeConfig("mise.toml", {
    ...config,
    NEW_HOST_URL: "https://{{env.GRAM_HOST}}/new",
  });
  const prompt = t.mock.fn(async () => true);
  await setupTailscale({ root: f.root, env: f.env, prompt });
  assert.equal(prompt.mock.callCount(), 1);
  assert.equal(f.read().NEW_HOST_URL, "https://{{env.GRAM_HOST}}/new");
});

test("completed setup refreshes drifted bind templates and is then idempotent", async (t) => {
  const f = fixture(t);
  await setupTailscale({ root: f.root, env: f.env, prompt: async () => true });
  const template = "{{env.GRAM_BIND_IP}}:{{env.NEW_SERVER_PORT}}";
  f.writeConfig("mise.toml", {
    NEW_SERVER_PORT: "34567",
    ...config,
    GRAM_SERVER_ADDRESS: template,
  });
  const prompt = t.mock.fn(async () => true);
  await setupTailscale({ root: f.root, env: f.env, prompt });
  assert.equal(prompt.mock.callCount(), 1);
  assert.equal(f.read().GRAM_SERVER_ADDRESS, template);
  const resolved = JSON.parse(
    execFileSync("mise", ["env", "--json"], {
      cwd: f.root,
      env: f.env,
      encoding: "utf8",
    }),
  );
  assert.equal(resolved.GRAM_SERVER_ADDRESS, `${ipv4}:34567`);
  const before = f.raw();
  await setupTailscale({ root: f.root, env: f.env, prompt });
  assert.equal(prompt.mock.callCount(), 1);
  assert.equal(f.raw(), before);
});

test("bind template drift preserves literal overrides without repeat prompts", async (t) => {
  const f = fixture(t);
  f.writeConfig("mise.local.toml", {
    ...f.local,
    GRAM_SERVER_ADDRESS: "192.0.2.10:23456",
  });
  await setupTailscale({ root: f.root, env: f.env, prompt: async () => true });
  f.writeConfig("mise.toml", {
    NEW_SERVER_PORT: "34567",
    ...config,
    GRAM_SERVER_ADDRESS: "{{env.GRAM_BIND_IP}}:{{env.NEW_SERVER_PORT}}",
  });
  const prompt = t.mock.fn(async () => true);
  const before = f.raw();
  await setupTailscale({ root: f.root, env: f.env, prompt });
  assert.equal(prompt.mock.callCount(), 0);
  assert.equal(f.raw(), before);
  await setupTailscale({
    root: f.root,
    env: { ...f.env, usage_force: "true" },
    prompt,
  });
  assert.equal(prompt.mock.callCount(), 1);
  assert.equal(f.read().GRAM_SERVER_ADDRESS, "192.0.2.10:23456");
});

test("existing hostname-only setup prompts to switch loopback to the Tailnet", async (t) => {
  const f = fixture(t);
  f.writeConfig("mise.local.toml", {
    ...f.local,
    GRAM_HOST: hostname,
    GRAM_SSL_EXTRA_HOSTS: `${hostname},${ipv4},${ipv6}`,
  });
  const prompt = t.mock.fn(async () => true);
  await setupTailscale({ root: f.root, env: f.env, prompt });
  assert.equal(prompt.mock.callCount(), 1);
  assert.equal(f.read().GRAM_BIND_IP, ipv4);
});

test("completed setup refreshes changed Tailnet IPs", async (t) => {
  const f = fixture(t);
  await setupTailscale({ root: f.root, env: f.env, prompt: async () => true });
  const prompt = t.mock.fn(async () => true);
  await setupTailscale({
    root: f.root,
    env: {
      ...f.env,
      TEST_STATUS: JSON.stringify({
        ...status,
        Self: { ...status.Self, TailscaleIPs: ["100.64.0.2", ipv6] },
      }),
    },
    prompt,
  });
  assert.equal(prompt.mock.callCount(), 1);
  assert.ok(f.read().GRAM_SSL_EXTRA_HOSTS.split(",").includes("100.64.0.2"));
  assert.equal(f.read().GRAM_BIND_IP, "100.64.0.2");
});

for (const operation of ["unset", "set"]) {
  test(`failed ${operation} leaves the original configuration untouched and cleans staging`, async (t) => {
    const f = fixture(t);
    const before = f.raw();
    miseWrapper(
      f.root,
      `
if (args[0] === ${JSON.stringify(operation)}) {
  fs.writeFileSync(file, '[env]\\nGRAM_HOST="partial"\\n');
  process.stderr.write('simulated config write failure');
  process.exit(1);
}`,
    );
    await assert.rejects(
      setupTailscale({ root: f.root, env: f.env, prompt: async () => true }),
      /simulated config write failure/,
    );
    assert.equal(f.raw(), before);
    assert.deepEqual(
      readdirSync(f.root).filter((name) => name.startsWith("mise.tailscale-")),
      [],
    );
  });
}

test("failed setup does not create a partial configuration when none existed", async (t) => {
  const f = fixture(t);
  unlinkSync(join(f.root, "mise.local.toml"));
  miseWrapper(
    f.root,
    `if (args[0] === 'set') { fs.writeFileSync(file, '[env]\\nGRAM_HOST="partial"\\n'); process.stderr.write('simulated config write failure'); process.exit(1); }`,
  );
  await assert.rejects(
    setupTailscale({ root: f.root, env: f.env, prompt: async () => true }),
    /simulated config write failure/,
  );
  assert.equal(existsSync(join(f.root, "mise.local.toml")), false);
  assert.deepEqual(
    readdirSync(f.root).filter((name) => name.startsWith("mise.tailscale-")),
    [],
  );
});

test("atomic replacement preserves configuration permissions", async (t) => {
  const f = fixture(t);
  const path = join(f.root, "mise.local.toml");
  chmodSync(path, 0o644);
  await setupTailscale({ root: f.root, env: f.env, prompt: async () => true });
  assert.equal(statSync(path).mode & 0o777, 0o644);
  assert.equal(f.read().GRAM_HOST, hostname);
});

test("atomic replacement does not overwrite detected concurrent edits", async (t) => {
  const f = fixture(t);
  const before = f.raw();
  miseWrapper(
    f.root,
    "",
    `if (args[0] === 'set') fs.appendFileSync(${JSON.stringify(join(f.root, "mise.local.toml"))}, '# concurrent edit\\n');`,
  );
  await assert.rejects(
    setupTailscale({ root: f.root, env: f.env, prompt: async () => true }),
    /changed during Tailscale setup/,
  );
  assert.equal(f.raw(), before + "# concurrent edit\n");
  assert.deepEqual(
    readdirSync(f.root).filter((name) => name.startsWith("mise.tailscale-")),
    [],
  );
});

for (const skipped of ["", "*", "zero:tailscale", "zero:tailscale-other"]) {
  test(`accepting refreshes dependency order and preserves ports (${skipped || "not skipped"})`, async (t) => {
    const f = fixture(t);
    let prompts = 0;
    await setupTailscale({
      root: f.root,
      env: {
        ...f.env,
        GRAM_ZERO_SKIP: skipped,
        usage_force:
          skipped === "*" || skipped === "zero:tailscale" ? "true" : "false",
      },
      prompt: async (host, ip) => {
        prompts++;
        assert.equal(host, hostname);
        assert.equal(ip, ipv4);
        return true;
      },
    });
    assert.equal(prompts, 1);
    const saved = f.read();
    assert.equal(saved.GRAM_HOST, hostname);
    assert.equal(saved.GRAM_BIND_IP, ipv4);
    assert.equal(
      saved.GRAM_SSL_EXTRA_HOSTS,
      `existing.example.com,${hostname},${ipv4},${ipv6}`,
    );
    assert.equal(saved.GRAM_SERVER_PORT, "23456");
    assert.equal(saved.GRAM_SITE_PORT, "23457");
    assert.equal(saved.UNRELATED, "keep");
    for (const [key, value] of dependents) assert.equal(saved[key], value);
    const resolved = JSON.parse(
      execFileSync("mise", ["env", "--json"], {
        cwd: f.root,
        env: f.env,
        encoding: "utf8",
      }),
    );
    assert.equal(resolved.GRAM_SITE_URL, `https://${hostname}:23457`);
    assert.equal(resolved.GRAM_APP_URL, `https://${hostname}:23456`);
    assert.equal(resolved.GRAM_ADMIN_BACKEND_URL, `https://${hostname}:23458`);
    assert.equal(
      resolved.GRAM_ADMIN_OIDC_EMULATOR_URL,
      `http://${hostname}:4000`,
    );
    assert.equal(resolved.WORKOS_API_URL, `http://${hostname}:35291/workos`);
    assert.equal(resolved.GRAM_SERVER_ADDRESS, `${ipv4}:23456`);
    assert.equal(resolved.GRAM_ADMIN_SERVER_ADDRESS, `${ipv4}:23458`);
    assert.equal(resolved.MOCK_OIDC_ADDRESS, `${ipv4}:4000`);
    assert.equal(resolved.GRAM_DEVIDP_ADDRESS, `${ipv4}:35291`);
    assert.equal(resolved.GRAM_DEVIDP_CONTROL_ADDRESS, "127.0.0.1:35292");
    const call = JSON.parse(readFileSync(f.calls, "utf8").trim());
    assert.deepEqual(call.args, ["status", "--json", "--peers=false"]);
    assert.equal(call.cli, "1");
  });
}

test("setup switches the shared bind address and preserves per-service overrides", async (t) => {
  const f = fixture(t);
  f.writeConfig("mise.local.toml", {
    ...f.local,
    GRAM_BIND_IP: "192.0.2.10",
    GRAM_DEVIDP_ADDRESS: "192.0.2.11:35291",
    GRAM_DEVIDP_CONTROL_ADDRESS: "192.0.2.12:35292",
  });
  await setupTailscale({ root: f.root, env: f.env, prompt: async () => true });
  const saved = f.read();
  assert.equal(saved.GRAM_HOST, hostname);
  assert.equal(saved.GRAM_BIND_IP, ipv4);
  assert.equal(saved.GRAM_DEVIDP_ADDRESS, "192.0.2.11:35291");
  assert.equal(saved.GRAM_DEVIDP_CONTROL_ADDRESS, "192.0.2.12:35292");
});

test("setup brackets an IPv6-only bind address", async (t) => {
  const f = fixture(t);
  await setupTailscale({
    root: f.root,
    env: {
      ...f.env,
      TEST_STATUS: JSON.stringify({
        ...status,
        Self: { ...status.Self, TailscaleIPs: [ipv6] },
      }),
    },
    prompt: async () => true,
  });
  const saved = f.read();
  assert.equal(saved.GRAM_HOST, hostname);
  assert.equal(saved.GRAM_BIND_IP, `[${ipv6}]`);
  const resolved = JSON.parse(
    execFileSync("mise", ["env", "--json"], {
      cwd: f.root,
      env: f.env,
      encoding: "utf8",
    }),
  );
  assert.equal(resolved.GRAM_SERVER_ADDRESS, `[${ipv6}]:23456`);
  assert.equal(resolved.GRAM_DEVIDP_ADDRESS, `[${ipv6}]:35291`);
  assert.equal(
    saved.GRAM_SSL_EXTRA_HOSTS,
    `existing.example.com,${hostname},${ipv6}`,
  );
});

test("dependency traversal deduplicates diamonds and ignores similarly named variables", () => {
  const result = dependentEnvVars(
    {
      LEAF: "{{env.LEFT}}/{{env.RIGHT}}",
      LEFT: "{{env.GRAM_HOST}}",
      RIGHT: "{{env.LEFT}}",
      END: "{{env.LEAF}}",
      BIND: "{{env.GRAM_BIND_IP}}",
      OTHER: "{{env.GRAM_HOST_SUFFIX}}",
      NUMBER: 8080,
    },
    ["GRAM_HOST", "GRAM_BIND_IP"],
  );
  assert.deepEqual(
    result.map(([key]) => key),
    ["LEFT", "RIGHT", "LEAF", "END", "BIND"],
  );
});

test("cyclic dependencies fail before overwriting settings", async (t) => {
  const f = fixture(t);
  f.writeConfig("mise.toml", {
    GRAM_HOST: "localhost",
    A: "{{env.GRAM_HOST}}/{{env.B}}",
    B: "{{env.A}}",
  });
  const before = f.raw();
  await assert.rejects(
    setupTailscale({ root: f.root, env: f.env, prompt: async () => true }),
    /Cyclic environment dependency/,
  );
  assert.equal(f.raw(), before);
});

test("zero runs interactive Tailscale setup before TLS", () => {
  const zero = readFileSync(new URL("../../zero", import.meta.url), "utf8");
  assert.match(
    zero,
    /interactive_only mise run zero:tailscale\s+mise run zero:tls/,
  );
});
