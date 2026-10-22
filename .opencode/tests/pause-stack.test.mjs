import assert from "node:assert/strict";
import childProcess from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { syncBuiltinESMExports } from "node:module";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { promisify } from "node:util";

const exec = promisify(childProcess.execFile);
let generation = 0;

async function fixture(t, options = {}) {
  const gitdir = await mkdtemp(join(tmpdir(), "opencode-pause-"));
  t.after(() => rm(gitdir, { recursive: true, force: true }));
  if (options.paused) await writeFile(join(gitdir, "gram-stack-paused"), "");
  if (options.boot)
    await writeFile(join(gitdir, "gram-stack-boot.pid"), "12345\n");

  const calls = [];
  const original = childProcess.execFile;
  childProcess.execFile = Object.assign(() => {}, {
    [promisify.custom]: async (file, args, opts) => {
      calls.push({ file, args, opts });
      if (file === "mise" && args[0] === "env") {
        assert.deepEqual(args, ["env", "--json"]);
        return {
          stdout: JSON.stringify({
            GRAM_OPENCODE_PLUGINS: "other:speakeasy.pause-stack",
          }),
        };
      }
      if (file === "git") {
        return {
          stdout: opts.cwd.startsWith("/worktree/")
            ? `${gitdir}\n`
            : "/other-gitdir\n",
        };
      }
      if (file === "ps") {
        if (options.boot === "dead")
          throw Object.assign(new Error("exited"), { code: 1 });
        return {
          stdout:
            options.boot === "live"
              ? "bash .mise-tasks/git/workboot.sh"
              : "unrelated-process",
        };
      }
      if (file === "opencode") {
        assert.deepEqual(args, ["api", "get", "/api/session/active"]);
        if (options.lookupError) throw new Error("activity lookup failed");
        return { stdout: JSON.stringify({ data: options.active ?? {} }) };
      }
      assert.equal(file, "mise");
      assert.deepEqual(args, ["run", "pause"]);
      await options.onPause?.();
      await writeFile(join(gitdir, "gram-stack-paused"), "");
      return { stdout: "" };
    },
  });
  syncBuiltinESMExports();
  t.after(() => {
    childProcess.execFile = original;
    syncBuiltinESMExports();
  });

  const { default: plugin } = await import(
    `../plugins/pause-stack.js?test=${generation++}`
  );
  const consumed = Promise.withResolvers();
  const location = { directory: "/worktree/root" };
  const sessions = {
    ses_finished: { projectID: "project", location },
    ses_other: {
      projectID: "project",
      location: options.otherLocation ?? location,
      parentID: options.parentID,
    },
  };
  const cleanup = await plugin.setup({
    location,
    session: { get: async ({ sessionID }) => sessions[sessionID] },
    event: {
      async *subscribe() {
        for (let i = 0; i < (options.events ?? 1); i++) {
          yield {
            type: "session.status",
            location,
            data: { sessionID: "ses_finished", status: { type: "idle" } },
          };
        }
        consumed.resolve();
      },
    },
  });
  t.after(cleanup);
  return { calls, consumed: consumed.promise };
}

const running = { type: "running" };
for (const [name, options, pauses] of [
  [
    "another session active before plugin load keeps the stack up",
    { active: { ses_other: running } },
    0,
  ],
  [
    "a session in a subdirectory shares the stack",
    {
      active: { ses_other: running },
      otherLocation: { directory: "/worktree/root/server" },
    },
    0,
  ],
  [
    "a different worktree does not block pausing",
    {
      active: { ses_other: running },
      otherLocation: { directory: "/another-worktree" },
    },
    1,
  ],
  [
    "a different workspace does not block pausing",
    {
      active: { ses_other: running },
      otherLocation: { directory: "/worktree/root", workspaceID: "remote" },
    },
    1,
  ],
  [
    "the ending session can still own its drain",
    { active: { ses_finished: running } },
    1,
  ],
  ["already-paused stacks are skipped", { paused: true }, 0],
  ["live initial boots are skipped", { boot: "live" }, 0],
  ["dead boot markers do not block pausing", { boot: "dead" }, 1],
  ["a reused boot PID does not block pausing", { boot: "reused" }, 1],
  ["a second idle event skips the now-paused stack", { events: 2 }, 1],
]) {
  test(name, async (t) => {
    const f = await fixture(t, options);
    await f.consumed;
    assert.equal(
      f.calls.filter((call) => call.file === "mise" && call.args[0] === "run")
        .length,
      pauses,
    );
  });
}

test("activity lookup failures leave the stack alone", async (t) => {
  const errors = [];
  t.mock.method(console, "error", (...args) => errors.push(args));
  const f = await fixture(t, { lookupError: true });
  await f.consumed;
  assert.equal(
    f.calls.some((call) => call.args[0] === "run"),
    false,
  );
  assert.equal(errors.length, 1);
});

test("event handling waits for the pause to finish", async (t) => {
  const started = Promise.withResolvers();
  const finish = Promise.withResolvers();
  const f = await fixture(t, {
    onPause: () => {
      started.resolve();
      return finish.promise;
    },
  });
  let consumed = false;
  void f.consumed.then(() => {
    consumed = true;
  });
  await started.promise;
  assert.equal(consumed, false);
  finish.resolve();
  await f.consumed;
  assert.equal(consumed, true);
});

test("the documented append preserves values and never writes after a failed read", async (t) => {
  const directory = await mkdtemp(join(tmpdir(), "opencode-append-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  await writeFile(
    join(directory, "mise"),
    `#!/bin/sh
if [ "$1" = env ] && [ "$2" = --json ]; then
  if [ "$READ_FAIL" = 1 ]; then
    echo 'simulated read failure' >&2
    exit 1
  fi
  printf '{"GRAM_OPENCODE_PLUGINS":"%s"}\\n' "$VALUE"
else
  printf '%s\\n' "$4"
fi
`,
    { mode: 0o755 },
  );
  const readme = await readFile(
    new URL("../README.md", import.meta.url),
    "utf8",
  );
  const command = readme.match(/```sh\n([\s\S]*?)\n```/)[1];
  for (const [value, readFail, expected] of [
    [
      "first:second",
      "0",
      "GRAM_OPENCODE_PLUGINS=first:second:speakeasy.pause-stack\n",
    ],
    ["", "0", "GRAM_OPENCODE_PLUGINS=speakeasy.pause-stack\n"],
    ["first:second", "1", ""],
  ]) {
    const { stdout, stderr } = await exec("sh", ["-c", command], {
      env: {
        ...process.env,
        PATH: `${directory}:${process.env.PATH}`,
        VALUE: value,
        READ_FAIL: readFail,
      },
    });
    assert.equal(stdout, expected);
    if (readFail === "1") assert.match(stderr, /simulated read failure/);
  }
});
