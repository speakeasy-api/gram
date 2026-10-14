import assert from "node:assert/strict";
import childProcess, { execFile } from "node:child_process";
import {
  access,
  mkdir,
  mkdtemp,
  readFile,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { syncBuiltinESMExports } from "node:module";
import { join } from "node:path";
import { test } from "node:test";
import { promisify } from "node:util";
import { makeStrategy } from "../lib/worktrunk.js";

const exec = promisify(execFile);

class OperationError extends Error {
  constructor({ message, forceRequired }) {
    super(message);
    this.forceRequired = forceRequired;
  }
}

test("Worktrunk lifecycle uses hooks, refs, ownership, and force semantics", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "worktrunk-test-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const repo = join(root, "repo");
  const config = join(root, "user.toml");
  await mkdir(join(repo, ".config"), { recursive: true });
  await writeFile(config, "");
  const env = {
    WORKTRUNK_CONFIG_PATH: config,
    WORKTRUNK_SYSTEM_CONFIG_PATH: config,
    WORKTRUNK_PROJECT_CONFIG_PATH: ".config/wt.toml",
    GIT_CONFIG_GLOBAL: config,
    GIT_CONFIG_NOSYSTEM: "1",
  };
  for (const [name, value] of Object.entries(env)) {
    const previous = process.env[name];
    process.env[name] = value;
    t.after(() => {
      if (previous === undefined) delete process.env[name];
      else process.env[name] = previous;
    });
  }
  const git = async (args, cwd = repo) =>
    (await exec("git", args, { cwd })).stdout.trim();
  await git(["init", "-b", "main"]);
  await git(["config", "user.email", "test@example.com"]);
  await git(["config", "user.name", "Test"]);
  await git(["config", "commit.gpgsign", "false"]);
  await writeFile(join(repo, ".gitignore"), "initialized\nfail-cleanup\n");
  await writeFile(
    join(repo, ".config/wt.toml"),
    `pre-start = "echo ready > initialized"
pre-remove = "test ! -f fail-cleanup && test -f initialized && echo cleaned > '{{ primary_worktree_path }}/../cleaned'"
`,
  );
  await git(["add", "."]);
  await git(["commit", "-m", "fixture"]);
  const base = await git(["rev-parse", "HEAD"]);
  await git(["commit", "--allow-empty", "-m", "next"]);

  const values = new Map();
  const storage = {
    get: async (key) => values.get(key),
    set: async (key, value) => values.set(key, value),
    remove: async (key) => values.delete(key),
  };
  const reload = () =>
    makeStrategy({ directory: repo, storage, OperationError });
  let strategy = reload();
  const context = { signal: new AbortController().signal };
  // Exercise spaces and literal template delimiters in the destination parent.
  const directory = join(root, "space {{ literal }}", "feature work");
  await mkdir(join(root, "space {{ literal }}"));
  assert.deepEqual(
    await strategy.create(
      { sourceDirectory: repo, directory, branch: base },
      context,
    ),
    { directory },
  );
  assert.equal(await git(["rev-parse", "HEAD"], directory), base);
  assert.equal(
    await readFile(join(directory, "initialized"), "utf8"),
    "ready\n",
  );
  const branch = await git(["branch", "--show-current"], directory);

  const other = join(root, "unowned");
  await git(["worktree", "add", "-b", "unowned", other]);
  strategy = reload();
  const inventory = await strategy.list(repo, context);
  assert.deepEqual(
    inventory.sort((a, b) => a.directory.localeCompare(b.directory)),
    [
      { directory: repo, type: "root" },
      { directory, type: "worktree" },
    ].sort((a, b) => a.directory.localeCompare(b.directory)),
  );
  await assert.rejects(
    strategy.remove({ directory: other, force: true }, context),
    /not owned/,
  );

  await writeFile(join(directory, "dirty"), "keep me");
  await assert.rejects(
    strategy.remove({ directory, force: false }, context),
    (error) => error instanceof OperationError && error.forceRequired,
  );
  await assert.rejects(access(join(root, "cleaned")));
  await access(join(directory, "dirty"));

  await writeFile(join(directory, "fail-cleanup"), "");
  await assert.rejects(strategy.remove({ directory, force: true }, context));
  await access(directory);
  assert.ok(
    (await strategy.list(repo, context)).some(
      (item) => item.directory === directory,
    ),
  );
  await rm(join(directory, "fail-cleanup"));
  await strategy.remove({ directory, force: true }, context);
  assert.equal(await readFile(join(root, "cleaned"), "utf8"), "cleaned\n");
  await assert.rejects(access(directory));
  await assert.rejects(git(["show-ref", "--verify", `refs/heads/${branch}`]));
  assert.deepEqual(await strategy.list(repo, context), [
    { directory: repo, type: "root" },
  ]);

  const second = join(root, "foo..bar");
  await strategy.create({ sourceDirectory: repo, directory: second }, context);
  assert.equal(
    await git(["rev-parse", "HEAD"], second),
    await git(["rev-parse", "HEAD"]),
  );
  await strategy.remove({ directory: second, force: false }, context);

  // Ownership survives a branch rename but not external removal and recreation,
  // even when both the branch and Git administrative path are reused.
  const reused = join(root, "reused");
  await strategy.create({ sourceDirectory: repo, directory: reused }, context);
  await git(["branch", "-m", "renamed"], reused);
  const gitdir = await git(["rev-parse", "--absolute-git-dir"], reused);
  assert.ok(
    (await reload().list(repo, context)).some(
      (item) => item.directory === reused,
    ),
  );
  await git(["worktree", "remove", "--force", reused]);
  await git(["worktree", "add", reused, "renamed"]);
  assert.equal(await git(["rev-parse", "--absolute-git-dir"], reused), gitdir);
  await rm(join(root, "cleaned"));
  strategy = reload();
  assert.deepEqual(await strategy.list(repo, context), [
    { directory: repo, type: "root" },
  ]);
  // A replacement marker must match the saved identity, not merely exist.
  await writeFile(
    join(gitdir, "speakeasy-opencode-worktree-id"),
    "replacement",
  );
  assert.deepEqual(await strategy.list(repo, context), [
    { directory: repo, type: "root" },
  ]);
  for (const force of [false, true]) {
    await assert.rejects(
      strategy.remove({ directory: reused, force }, context),
      /not owned/,
    );
  }
  await access(join(reused, ".git"));
  await assert.rejects(access(join(root, "cleaned")));

  const controller = new AbortController();
  controller.abort();
  await assert.rejects(strategy.list(repo, { signal: controller.signal }), {
    name: "AbortError",
  });

  // Abort at the boundary where the real wt process has completed successfully,
  // before the strategy can start ownership bookkeeping.
  const completed = new AbortController();
  const original = childProcess.execFile;
  childProcess.execFile = Object.assign((...args) => original(...args), {
    [promisify.custom]: (file, args, options) => {
      const pending = exec(file, args, options);
      const result = pending.then((output) => {
        if (file === "wt" && args.includes("switch")) completed.abort();
        return output;
      });
      result.child = pending.child;
      return result;
    },
  });
  syncBuiltinESMExports();
  try {
    const { makeStrategy: cancelAfterCreate } =
      await import("../lib/worktrunk.js?cancel-after-create");
    const canceled = join(root, "canceled-after-create");
    await cancelAfterCreate({
      directory: repo,
      storage,
      OperationError,
    }).create(
      { sourceDirectory: repo, directory: canceled },
      { signal: completed.signal },
    );
    assert.equal(completed.signal.aborted, true);
    assert.ok(
      (await reload().list(repo, context)).some(
        (item) => item.directory === canceled,
      ),
    );
    await reload().remove({ directory: canceled, force: false }, context);
    await assert.rejects(access(canceled));
  } finally {
    childProcess.execFile = original;
    syncBuiltinESMExports();
  }
});
