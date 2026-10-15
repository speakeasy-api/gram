import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { promisify } from "node:util";

const exec = promisify(execFile);
const hook = new URL("../session-hooks/pause-stack.sh", import.meta.url)
  .pathname;

const fakes = {
  mise: `#!/bin/sh
if [ "$1" = env ] && [ "$2" = --json ]; then
  [ "$MISE_FAIL" = 1 ] && exit 1
  printf '{"GRAM_OPENCODE_PLUGINS":%s}\\n' "$PLUGINS"
else
  echo "mise $*" >> "$CALLS"
fi
`,
  pitchfork: `#!/bin/sh
[ "$PITCHFORK_FAIL" = 1 ] && exit 1
echo "pitchfork $*" >> "$CALLS"
`,
  ps: `#!/bin/sh
[ -n "$PS_OUT" ] || exit 1
echo "$PS_OUT"
`,
};

async function fixture(t, { localConfig = true } = {}) {
  const dir = await mkdtemp(join(tmpdir(), "claude-pause-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const root = join(dir, "worktree");
  const bin = join(dir, "bin");
  await mkdir(join(root, "server"), { recursive: true });
  await mkdir(bin);
  await exec("git", ["init", "-q", root]);
  if (localConfig) await writeFile(join(root, "mise.local.toml"), "");
  for (const [name, body] of Object.entries(fakes)) {
    await writeFile(join(bin, name), body, { mode: 0o755 });
  }
  const calls = join(dir, "calls");
  await writeFile(calls, "");
  const gitdir = join(root, ".git");

  return {
    gitdir,
    async run(event, session, extra = {}, env = {}) {
      const child = exec(hook, {
        env: {
          ...process.env,
          PATH: `${bin}:${process.env.PATH}`,
          CALLS: calls,
          PLUGINS: JSON.stringify("other: speakeasy.pause-stack "),
          ...env,
        },
      });
      child.child.stdin.end(
        JSON.stringify({
          hook_event_name: event,
          session_id: session,
          cwd: join(root, "server"),
          ...(event === "Stop" ? { background_tasks: [] } : {}),
          ...extra,
        }),
      );
      await child;
    },
    async calls() {
      return (await readFile(calls, "utf8")).split("\n").filter(Boolean);
    },
  };
}

const pausedByPitchfork = ["pitchfork start pause-stack"];

test("a settled turn pauses the stack through pitchfork", async (t) => {
  const f = await fixture(t);
  await f.run("UserPromptSubmit", "a");
  await f.run("Stop", "a");
  assert.deepEqual(await f.calls(), pausedByPitchfork);
});

test("without a supervisor the stack is paused directly", async (t) => {
  const f = await fixture(t);
  await f.run("Stop", "a", {}, { PITCHFORK_FAIL: "1" });
  assert.deepEqual(await f.calls(), ["mise run pause"]);
});

for (const [name, value, enabled] of [
  ["partial IDs do not enable the hook", "speakeasy.pause-stack-extra", false],
  [
    "comma-separated IDs do not enable the hook",
    "a,speakeasy.pause-stack",
    false,
  ],
  ["an empty value disables the hook", "", false],
  ["an unset variable disables the hook", null, false],
  ["an invalid value disables the hook", 42, false],
]) {
  test(name, async (t) => {
    const f = await fixture(t);
    await f.run(
      "UserPromptSubmit",
      "a",
      {},
      { PLUGINS: JSON.stringify(value) },
    );
    await f.run("Stop", "a", {}, { PLUGINS: JSON.stringify(value) });
    assert.deepEqual(await f.calls(), enabled ? pausedByPitchfork : []);
  });
}

test("a failed lookup disables the hook", async (t) => {
  const f = await fixture(t);
  await f.run("Stop", "a", {}, { MISE_FAIL: "1" });
  assert.deepEqual(await f.calls(), []);
});

test("a worktree without its own mise.local.toml is left alone", async (t) => {
  const f = await fixture(t, { localConfig: false });
  await f.run("Stop", "a");
  assert.deepEqual(await f.calls(), []);
});

test("background work defers the pause to the turn that settles it", async (t) => {
  const f = await fixture(t);
  const running = [{ id: "t1", type: "subagent", status: "running" }];
  await f.run("Stop", "a", { background_tasks: running });
  assert.deepEqual(await f.calls(), []);
  await f.run("Stop", "a");
  assert.deepEqual(await f.calls(), pausedByPitchfork);
});

test("a missing task registry leaves the stack alone", async (t) => {
  const f = await fixture(t);
  await f.run("Stop", "a", { background_tasks: undefined });
  assert.deepEqual(await f.calls(), []);
});

test("scheduled wakeups do not keep the stack up", async (t) => {
  const f = await fixture(t);
  const crons = [{ id: "c1", schedule: "0 9 * * *", recurring: false }];
  await f.run("Stop", "a", { session_crons: crons });
  assert.deepEqual(await f.calls(), pausedByPitchfork);
});

test("the last session in the worktree to settle pauses", async (t) => {
  const f = await fixture(t);
  await f.run("UserPromptSubmit", "a");
  await f.run("UserPromptSubmit", "b");
  await f.run("Stop", "a");
  assert.deepEqual(await f.calls(), []);
  await f.run("Stop", "b");
  assert.deepEqual(await f.calls(), pausedByPitchfork);
});

test("a closed session no longer holds the stack up", async (t) => {
  const f = await fixture(t);
  await f.run("UserPromptSubmit", "b");
  await f.run("SessionEnd", "b");
  await f.run("Stop", "a");
  assert.deepEqual(await f.calls(), pausedByPitchfork);
});

test("already-paused stacks are skipped", async (t) => {
  const f = await fixture(t);
  await writeFile(join(f.gitdir, "gram-stack-paused"), "");
  await f.run("Stop", "a");
  assert.deepEqual(await f.calls(), []);
});

for (const [name, psOut, pauses] of [
  ["live initial boots are skipped", "bash .mise-tasks/git/workboot.sh", false],
  ["dead boot markers do not block pausing", "", true],
  ["a reused boot PID does not block pausing", "unrelated-process", true],
]) {
  test(name, async (t) => {
    const f = await fixture(t);
    await writeFile(join(f.gitdir, "gram-stack-boot.pid"), "12345\n");
    await f.run("Stop", "a", {}, { PS_OUT: psOut });
    assert.deepEqual(await f.calls(), pauses ? pausedByPitchfork : []);
  });
}

test("unsafe session IDs are ignored", async (t) => {
  const f = await fixture(t);
  await f.run("UserPromptSubmit", "../../escape");
  await f.run("Stop", "a");
  assert.deepEqual(await f.calls(), pausedByPitchfork);
});
