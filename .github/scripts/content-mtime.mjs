#!/usr/bin/env node

// Gives every tracked file and directory a modification time derived from its
// git object id, so the time changes exactly when the content does.
//
// `go test` caches a package's result against the files its tests open or
// stat at run time, and identifies those files by size and modification time
// rather than content (cmd/go/internal/test: hashOpen, hashWriteStat). A fresh
// checkout stamps every file with the current time, so on CI any package whose
// tests read a repository file (schema.sql, an OpenAPI document, testdata)
// could never reuse a cached result. With content-derived times those
// packages are cached like the rest, and still re-run when a file they read
// changes.
//
// Run it after the last step that creates files in the checkout and right
// before `go test`: creating a file updates its directory's time.

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

// 2001-01-01T00:00:00Z. Times land in the twenty years after it, far enough
// in the past that Go never treats a file as too recently written to cache.
const EPOCH_SECONDS = 978307200;
const WINDOW_SECONDS = 20 * 365 * 24 * 60 * 60;

/** Maps a git object id to a time in seconds, with millisecond precision. */
export function mtimeForObject(oid) {
  const seconds = Number.parseInt(oid.slice(0, 12), 16) % WINDOW_SECONDS;
  const millis = Number.parseInt(oid.slice(12, 20), 16) % 1000;
  return EPOCH_SECONDS + seconds + millis / 1000;
}

/** Parses `git ls-tree -r -t -z` output into [{ type, oid, file }]. */
export function parseTree(output) {
  const entries = [];
  for (const record of output.split("\0")) {
    if (record === "") continue;
    const tab = record.indexOf("\t");
    const [mode, type, oid] = record.slice(0, tab).split(" ");
    entries.push({ mode, type, oid, file: record.slice(tab + 1) });
  }
  return entries;
}

function git(root, args) {
  return execFileSync("git", args, {
    cwd: root,
    encoding: "utf8",
    maxBuffer: 1 << 30,
  });
}

export function applyContentMtimes(root) {
  const entries = parseTree(git(root, ["ls-tree", "-r", "-t", "-z", "HEAD"]));
  let count = 0;

  const touch = (file, oid) => {
    const time = mtimeForObject(oid);
    try {
      fs.utimesSync(path.join(root, file), time, time);
      count++;
    } catch (err) {
      // A tracked path that is absent from the working tree (sparse checkout,
      // or a step that deleted it) has nothing to stamp.
      if (err.code !== "ENOENT") throw err;
    }
  };

  // Symlinks are skipped: utimes would follow them to a target that is
  // stamped under its own path. Submodule entries ("commit") have no content
  // here.
  for (const entry of entries) {
    if (entry.type === "blob" && entry.mode !== "120000") {
      touch(entry.file, entry.oid);
    }
  }
  for (const entry of entries) {
    if (entry.type === "tree") touch(entry.file, entry.oid);
  }
  touch(".", git(root, ["rev-parse", "HEAD^{tree}"]).trim());

  return count;
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const root = git(process.cwd(), ["rev-parse", "--show-toplevel"]).trim();
  const started = Date.now();
  const count = applyContentMtimes(root);
  console.log(
    `Set content-derived modification times on ${count} paths in ${Date.now() - started}ms`,
  );
}
