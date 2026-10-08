import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import {
  mkdtempSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { test, type TestContext } from "node:test";
import { parse } from "jsonc-parser";

const task = fileURLToPath(new URL("./workmcp.mts", import.meta.url));

function fixture(t: TestContext) {
  const root = mkdtempSync(join(tmpdir(), "workmcp-test-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const bin = join(root, "bin");
  const home = join(root, "home");
  mkdirSync(bin);
  mkdirSync(home);
  const worktree = join(root, "worktree");
  execFileSync("git", ["init", "--quiet", worktree]);
  const configFile = join(home, ".claude.json");
  const calls = join(root, "calls.jsonl");
  writeFileSync(configFile, JSON.stringify({ projects: {} }));
  writeFileSync(
    join(bin, "claude"),
    `#!${process.execPath}
const fs = require('node:fs');
const file = process.env.CLAUDE_CONFIG_DIR + '/.claude.json';
const config = JSON.parse(fs.readFileSync(file, 'utf8'));
const args = process.argv.slice(2);
fs.appendFileSync(process.env.CALLS, JSON.stringify({ cwd: process.cwd(), args }) + '\\n');
if (process.env.FAIL_REMOVE === 'true' && args[1] === 'remove') process.exit(1);
const servers = (config.projects[process.cwd()] ??= {}).mcpServers ??= {};
if (args[1] === 'add') {
  if (servers[args[6]]) process.exit(1);
  servers[args[6]] = { type: 'http', url: args[7] };
} else if (args[1] === 'add-json') {
  servers[args[4]] = JSON.parse(args[5]);
} else if (args[1] === 'remove') {
  delete servers[args[4]];
} else process.exit(2);
fs.writeFileSync(file, JSON.stringify(config));
`,
    { mode: 0o755 },
  );
  const env = {
    ...process.env,
    PATH: `${bin}:/usr/bin:/bin`,
    CLAUDE_CONFIG_DIR: home,
    CALLS: calls,
    usage_server_url: "https://localhost:23456",
    usage_out_file: join(worktree, ".opencode/opencode.jsonc"),
    usage_remove: "false",
    usage_worktree: "",
  };
  const run = (overrides: Record<string, string> = {}, cwd = worktree) =>
    spawnSync(process.execPath, [task], {
      cwd,
      env: { ...env, ...overrides },
      encoding: "utf8",
    });
  const ok = (overrides: Record<string, string> = {}, cwd = worktree) => {
    const result = run(overrides, cwd);
    assert.equal(result.status, 0, result.stderr);
  };
  const read = () => JSON.parse(readFileSync(configFile, "utf8"));
  const seed = (servers: Record<string, unknown>) => {
    const config = read();
    config.projects[worktree] = { mcpServers: servers };
    writeFileSync(configFile, JSON.stringify(config));
  };
  return { root, bin, worktree, configFile, calls, env, run, ok, read, seed };
}

test("OpenCode V2 upsert preserves JSONC comments, custom options and other servers", (t) => {
  const f = fixture(t);
  const file = f.env.usage_out_file;
  mkdirSync(join(f.worktree, ".opencode"));
  writeFileSync(
    file,
    `{
  // Personal defaults
  "model": "example/model",
  "mcp": { "servers": {
    "other": { "type": "remote", "url": "https://example.com/mcp", },
    "platform": {
      // Keep this server disabled until needed
      "disabled": true,
      "url": "https://localhost:12345/platform-mcp",
    },
  } },
}
`,
  );
  f.ok();
  const source = readFileSync(file, "utf8");
  assert.match(source, /\/\/ Personal defaults/);
  assert.match(source, /\/\/ Keep this server disabled until needed/);
  assert.match(source, /"https:\/\/example.com\/mcp",/);
  const config = parse(source);
  assert.equal(config.model, "example/model");
  assert.equal(config.mcp.servers.other.url, "https://example.com/mcp");
  assert.equal(config.mcp.servers.platform.disabled, true);
  assert.equal(
    config.mcp.servers.platform.url,
    "https://localhost:23456/platform-mcp",
  );
  f.ok();
  assert.equal(readFileSync(file, "utf8"), source);
});

test("rejects HTTP and malformed JSONC before mutating either harness", (t) => {
  const f = fixture(t);
  mkdirSync(join(f.worktree, ".opencode"));
  const file = f.env.usage_out_file;
  writeFileSync(file, "{}");
  for (const url of [
    "http://example.com",
    "http://localhost:8080",
    "ftp://example.com",
  ]) {
    assert.notEqual(f.run({ usage_server_url: url }).status, 0);
    assert.equal(readFileSync(file, "utf8"), "{}");
  }
  writeFileSync(file, "{ broken");
  assert.notEqual(f.run().status, 0);
  assert.equal(readFileSync(file, "utf8"), "{ broken");
  assert.deepEqual(f.read().projects, {});
});

test("cleanup keeps pre-existing registrations, including after a port update", (t) => {
  const f = fixture(t);
  f.seed({
    platform: {
      type: "http",
      url: "https://localhost:23456/platform-mcp",
      headers: { "X-Test": "keep" },
    },
  });
  f.ok();
  f.ok({ usage_remove: "true" });
  assert.ok(f.read().projects[f.worktree].mcpServers.platform);
  f.ok({ usage_server_url: "https://localhost:23457" });
  f.ok({ usage_remove: "true" });
  const platform = f.read().projects[f.worktree].mcpServers.platform;
  assert.equal(platform.url, "https://localhost:23457/platform-mcp");
  assert.deepEqual(platform.headers, { "X-Test": "keep" });
});

test("owned cleanup targets only the selected project using the trusted task", (t) => {
  const f = fixture(t);
  f.ok();
  f.ok({ usage_server_url: "https://localhost:23457" });
  const config = f.read();
  config.projects[f.worktree].mcpServers.other = {
    type: "http",
    url: "https://example.com/other",
  };
  config.projects[f.root] = {
    mcpServers: {
      platform: { type: "http", url: "https://example.com/current" },
    },
  };
  writeFileSync(f.configFile, JSON.stringify(config));
  // A target checkout's executable task must never be resolved during cleanup.
  mkdirSync(join(f.worktree, ".mise-tasks/git"), { recursive: true });
  writeFileSync(
    join(f.worktree, ".mise-tasks/git/workmcp.mts"),
    'throw new Error("untrusted task executed");',
  );
  f.ok({ usage_remove: "true", usage_worktree: f.worktree }, f.root);
  f.ok({ usage_remove: "true", usage_worktree: f.worktree }, f.root);
  const after = f.read();
  assert.equal(after.projects[f.worktree].mcpServers.platform, undefined);
  assert.ok(after.projects[f.worktree].mcpServers.other);
  assert.ok(after.projects[f.root].mcpServers.platform);
  const removals = readFileSync(f.calls, "utf8")
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line))
    .filter((call) => call.args[1] === "remove");
  assert.ok(removals.every((call) => call.cwd === f.worktree));
});

test("cleanup preserves a user-repointed registration and reports retryable failures", (t) => {
  const f = fixture(t);
  f.ok();
  const failure = f.run({ usage_remove: "true", FAIL_REMOVE: "true" });
  assert.notEqual(failure.status, 0);
  assert.ok(failure.stderr.includes(f.worktree));
  f.ok({ usage_remove: "true" });
  assert.equal(f.read().projects[f.worktree].mcpServers.platform, undefined);
  f.ok();
  f.seed({ platform: { type: "http", url: "https://example.com/personal" } });
  f.ok({ usage_remove: "true" });
  assert.equal(
    f.read().projects[f.worktree].mcpServers.platform.url,
    "https://example.com/personal",
  );
});

test("workinit continues after optional MCP setup fails", (t) => {
  const f = fixture(t);
  const log = join(f.root, "mise.log");
  writeFileSync(
    join(f.bin, "mise"),
    `#!/bin/sh
echo "$*" >> "$MISE_TEST_LOG"
case "$*" in
  "run git:workmcp") exit 1 ;;
esac
`,
    { mode: 0o755 },
  );
  const result = spawnSync(
    "bash",
    [fileURLToPath(new URL("./workinit.sh", import.meta.url))],
    {
      cwd: f.worktree,
      env: { ...f.env, MISE_TEST_LOG: log },
      encoding: "utf8",
    },
  );
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stderr, /MCP setup failed/);
  assert.match(readFileSync(log, "utf8"), /run zero:tunnel-identity/);
});
