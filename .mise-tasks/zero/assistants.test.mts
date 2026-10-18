import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test, type TestContext } from "node:test";
import { fileURLToPath } from "node:url";

const task = fileURLToPath(new URL("./assistants.mts", import.meta.url));
const providerKey = "GRAM_ASSISTANT_RUNTIME_PROVIDER";
const imageKey = "GRAM_ASSISTANT_RUNTIME_OCI_IMAGE";

function fixture(t: TestContext, config?: string) {
  const root = mkdtempSync(join(tmpdir(), "assistant-bootstrap-test-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const bin = join(root, "bin");
  mkdirSync(bin);
  const configPath = join(root, "mise.local.toml");
  if (config !== undefined) writeFileSync(configPath, config);
  const callsPath = join(root, "calls.jsonl");
  // Stub external commands: no real configuration or Docker state is touched.
  for (const name of ["mise", "docker"]) {
    writeFileSync(
      join(bin, name),
      `#!${process.execPath}
require('node:fs').appendFileSync(process.env.CALLS, JSON.stringify({name: ${JSON.stringify(name)}, args: process.argv.slice(2)}) + '\\n');
`,
      { mode: 0o755 },
    );
  }
  function run(overrides: NodeJS.ProcessEnv = {}) {
    const result = spawnSync(process.execPath, [task], {
      cwd: root,
      env: {
        PATH: `${bin}:/usr/bin:/bin`,
        HOME: root,
        CALLS: callsPath,
        usage_skip_image: "true",
        ...overrides,
      },
      encoding: "utf8",
    });
    const calls = existsSync(callsPath)
      ? readFileSync(callsPath, "utf8")
          .trim()
          .split("\n")
          .map((line) => JSON.parse(line) as { name: string; args: string[] })
      : [];
    return { result, calls };
  }
  return { run, configPath };
}

for (const source of ["file", "environment"] as const) {
  test(`rejects obsolete Fly provider from ${source} before writing configuration`, (t) => {
    const config =
      source === "file" ? `[env]\n${providerKey} = "flyio"\n` : undefined;
    const f = fixture(t, config);
    const { result, calls } = f.run(
      source === "environment" ? { [providerKey]: "flyio" } : {},
    );
    assert.notEqual(result.status, 0);
    assert.match(
      result.stderr,
      /Unsupported assistant runtime provider "flyio"/,
    );
    assert.match(result.stderr, /mise run zero:assistants --restart/);
    assert.deepEqual(calls, []);
    if (config !== undefined)
      assert.equal(readFileSync(f.configPath, "utf8"), config);
    else assert.equal(existsSync(f.configPath), false);
  });
}

for (const provider of ["gke", "local"]) {
  test(`preserves an intentional ${provider} configuration`, (t) => {
    const config = `[env]\n${providerKey} = "${provider}"\n${imageKey} = "custom-runtime"\n`;
    const f = fixture(t, config);
    const { result, calls } = f.run();
    assert.equal(result.status, 0, result.stderr);
    assert.deepEqual(calls, []);
    assert.equal(readFileSync(f.configPath, "utf8"), config);
  });

  test(`persists an intentional ${provider} environment override`, (t) => {
    const f = fixture(t);
    const { result, calls } = f.run({
      [providerKey]: provider,
      [imageKey]: "custom-runtime",
    });
    assert.equal(result.status, 0, result.stderr);
    assert.deepEqual(
      calls.map((call) => call.args.at(-1)),
      [`${providerKey}=${provider}`, `${imageKey}=custom-runtime`],
    );
  });
}

test("explicit restart resets obsolete provider and image to local defaults", (t) => {
  const f = fixture(
    t,
    `[env]\n${providerKey} = "flyio"\n${imageKey} = "obsolete-runtime"\n`,
  );
  const { result, calls } = f.run({
    usage_restart: "true",
    [providerKey]: "flyio",
  });
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(
    calls.map((call) => call.args.at(-1)),
    [`${providerKey}=local`, `${imageKey}=gram-assistant-runtime`],
  );
});

test("fresh setup uses local defaults", (t) => {
  const { result, calls } = fixture(t).run();
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(
    calls.map((call) => call.args.at(-1)),
    [`${providerKey}=local`, `${imageKey}=gram-assistant-runtime`],
  );
});
