import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";

import {
  applyContentMtimes,
  mtimeForObject,
  parseTree,
} from "./content-mtime.mjs";

function repo() {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "content-mtime-"));
  const git = (...args) =>
    execFileSync("git", args, { cwd: root, encoding: "utf8" });
  git("init", "--quiet");
  git("config", "user.email", "ci@example.com");
  git("config", "user.name", "ci");
  git("config", "commit.gpgsign", "false");
  return { root, git };
}

function commit(git, message) {
  git("add", "--all");
  git("commit", "--quiet", "--message", message);
}

const mtime = (root, file) => fs.statSync(path.join(root, file)).mtimeMs;

test("mtimeForObject is deterministic and old enough to cache", () => {
  const oid = "0123456789abcdef0123456789abcdef01234567";
  assert.equal(mtimeForObject(oid), mtimeForObject(oid));
  assert.notEqual(mtimeForObject(oid), mtimeForObject(oid.replace("0", "f")));
  assert.ok(mtimeForObject("f".repeat(40)) < Date.UTC(2022, 0, 1) / 1000);
  assert.ok(mtimeForObject("0".repeat(40)) >= Date.UTC(2001, 0, 1) / 1000);
});

test("parseTree keeps paths with spaces and tabs intact", () => {
  const entries = parseTree(
    "100644 blob aaaa\tdir/a file.txt\x00040000 tree bbbb\tdir\0",
  );
  assert.deepEqual(entries, [
    { mode: "100644", type: "blob", oid: "aaaa", file: "dir/a file.txt" },
    { mode: "040000", type: "tree", oid: "bbbb", file: "dir" },
  ]);
});

test("times follow content, not checkout time", () => {
  const { root, git } = repo();
  fs.mkdirSync(path.join(root, "a/b"), { recursive: true });
  fs.writeFileSync(path.join(root, "a/b/one.txt"), "one");
  fs.writeFileSync(path.join(root, "a/two.txt"), "two");
  fs.writeFileSync(path.join(root, "three.txt"), "three");
  commit(git, "first");

  applyContentMtimes(root);
  const before = Object.fromEntries(
    [".", "a", "a/b", "a/b/one.txt", "a/two.txt", "three.txt"].map((f) => [
      f,
      mtime(root, f),
    ]),
  );

  // Re-stamping the same tree after files were rewritten restores the times.
  fs.writeFileSync(path.join(root, "a/b/one.txt"), "one");
  applyContentMtimes(root);
  for (const [file, time] of Object.entries(before)) {
    assert.equal(mtime(root, file), time, file);
  }

  // Same-size edit: the file and every directory above it change, the rest
  // keep their times.
  fs.writeFileSync(path.join(root, "a/b/one.txt"), "eno");
  commit(git, "second");
  applyContentMtimes(root);
  for (const file of [".", "a", "a/b", "a/b/one.txt"]) {
    assert.notEqual(mtime(root, file), before[file], file);
  }
  for (const file of ["a/two.txt", "three.txt"]) {
    assert.equal(mtime(root, file), before[file], file);
  }
});

test("symlinks and missing files are skipped", () => {
  const { root, git } = repo();
  fs.writeFileSync(path.join(root, "target.txt"), "target");
  fs.symlinkSync("target.txt", path.join(root, "link.txt"));
  fs.writeFileSync(path.join(root, "gone.txt"), "gone");
  commit(git, "first");
  fs.rmSync(path.join(root, "gone.txt"));

  applyContentMtimes(root);
  const blob = git("rev-parse", "HEAD:target.txt").trim();
  assert.equal(mtime(root, "target.txt"), mtimeForObject(blob) * 1000);
});
