#!/usr/bin/env node

//MISE description="Squash generated-file changes into a labeled commit. --amend folds into HEAD (must be mig:/gen:/chore(gen):). --drop scrubs without regenerating."
//MISE dir="{{ config_root }}"

//USAGE flag "--base <ref>" help="Base ref scoping commits to rewrite (default: merge-base with origin/main)"
//USAGE flag "--amend" help="Fold regenerated artifacts into HEAD instead of creating a new commit"
//USAGE flag "--drop" help="Scrub generated changes from scope without running generators"

import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { $ } from "zx";

$.verbose = false;

// ---------- types & constants -------------------------------------------------

interface Options {
  base?: string;
  amend: boolean;
  drop: boolean;
}

const LABELED_SUBJECT = /^(mig|gen|chore\(gen\)):/;

const REGEN_SUBJECT = "chore(gen): regenerate artifacts";

// ---------- small helpers -----------------------------------------------------

function info(msg: string): void {
  console.error(msg);
}

function fail(msg: string): never {
  console.error(`error: ${msg}`);
  process.exit(1);
}

async function git(...args: string[]): Promise<string> {
  const out = await $`git ${args}`.nothrow();
  if (out.exitCode !== 0) {
    fail(`git ${args.join(" ")} failed:\n${out.stderr}`);
  }
  return out.stdout.replace(/\n$/, "");
}

async function gitMaybe(...args: string[]): Promise<string | null> {
  const out = await $`git ${args}`.nothrow();
  return out.exitCode === 0 ? out.stdout.replace(/\n$/, "") : null;
}

function loadOptions(): Options {
  const amend = process.env["usage_amend"] === "true";
  const drop = process.env["usage_drop"] === "true";
  if (amend && drop) fail("--amend and --drop are mutually exclusive");
  return { base: process.env["usage_base"] || undefined, amend, drop };
}

// ---------- precondition checks ----------------------------------------------

async function assertCleanWorktree(): Promise<void> {
  const status = await git("status", "--porcelain", "--untracked-files=no");
  if (status) {
    fail(
      "working tree has uncommitted tracked changes; commit or stash before running",
    );
  }

  // Untracked generated files would be swept into the chore(gen) commit by the
  // staging step (which globs untracked paths), polluting history with artifacts
  // that aren't part of the rewritten scope. Reject them up front so the rewrite
  // stays deterministic. Unrelated untracked files (scratch notes, etc.) are fine.
  const untracked = (await git("ls-files", "--others", "--exclude-standard"))
    .split("\n")
    .filter(Boolean);
  const untrackedGenerated = await filterIgnored(
    await filterLinguistGenerated(untracked),
  );
  if (untrackedGenerated.length > 0) {
    fail(
      "untracked generated files present; remove or commit them before running:\n" +
        untrackedGenerated.map((p) => `  ${p}`).join("\n"),
    );
  }
}

async function subjectOf(commit: string): Promise<string> {
  return git("log", "-1", "--format=%s", commit);
}

async function isLabeledCommit(commit: string): Promise<boolean> {
  return LABELED_SUBJECT.test(await subjectOf(commit));
}

async function assertNoMergeCommits(scopeStart: string): Promise<void> {
  const commits = (await git("rev-list", `${scopeStart}..HEAD`))
    .split("\n")
    .filter(Boolean);
  for (const c of commits) {
    const raw = await git("cat-file", "-p", c);
    const parents = raw
      .split("\n")
      .filter((l) => l.startsWith("parent ")).length;
    if (parents > 1) {
      fail(
        `merge commit in scope: ${c.slice(0, 7)} ${await subjectOf(c)} — refusing`,
      );
    }
  }
}

// ---------- scope resolution -------------------------------------------------

async function resolveBase(opts: Options): Promise<string> {
  if (opts.base) {
    const r = await gitMaybe("rev-parse", "--verify", `${opts.base}^{commit}`);
    if (!r) fail(`--base '${opts.base}' does not resolve to a commit`);
    return r;
  }
  if (!(await gitMaybe("rev-parse", "--verify", "origin/main"))) {
    fail("no origin/main; pass --base <ref>");
  }
  const base = await gitMaybe("merge-base", "HEAD", "origin/main");
  if (!base) fail("could not find merge-base with origin/main");
  return base;
}

/** Walk base..HEAD oldest-first; advance scope start past every labeled ancestor. */
async function resolveScopeStart(base: string): Promise<string> {
  const commits = (await git("rev-list", "--reverse", `${base}..HEAD`))
    .split("\n")
    .filter(Boolean);
  let scopeStart = base;
  for (const c of commits) {
    if (await isLabeledCommit(c)) scopeStart = c;
  }
  return scopeStart;
}

// ---------- generated-path discovery -----------------------------------------

/** Pipe a list of paths through `git check-attr --stdin linguist-generated` and return those flagged `set`. */
async function filterLinguistGenerated(paths: string[]): Promise<string[]> {
  if (paths.length === 0) return [];
  const input = paths.join("\n") + "\n";
  const result = await $({ input })`git check-attr --stdin linguist-generated`;
  return result.stdout
    .split("\n")
    .filter((line) => line.endsWith(": linguist-generated: set"))
    .map((line) => line.replace(/: linguist-generated: set$/, ""));
}

/**
 * Drop any paths matched by .gitignore — `git add` rejects them and aborts the batch.
 * `--no-index` is essential: without it, check-ignore silently skips tracked files, but
 * `git add` still refuses tracked files whose path matches a gitignore rule (orphaned
 * generated content that pre-dated the ignore rule).
 */
async function filterIgnored(paths: string[]): Promise<string[]> {
  if (paths.length === 0) return [];
  const input = paths.join("\n") + "\n";
  const result = await $({
    input,
    nothrow: true,
  })`git check-ignore --stdin --no-index`;
  // exit 0 = some ignored, 1 = none ignored, 128 = error
  if (result.exitCode !== 0 && result.exitCode !== 1) {
    fail(`git check-ignore failed:\n${result.stderr}`);
  }
  const ignored = new Set(result.stdout.split("\n").filter(Boolean));
  return paths.filter((p) => !ignored.has(p));
}

/** Union of linguist-generated paths across the given commits, using the working-tree .gitattributes. */
async function collectGeneratedPaths(commits: string[]): Promise<string[]> {
  const all = new Set<string>();
  for (const c of commits) {
    const paths = (await git("ls-tree", "-r", "--name-only", c))
      .split("\n")
      .filter(Boolean);
    paths.forEach((p) => all.add(p));
  }
  return filterLinguistGenerated([...all]);
}

// ---------- per-commit tree surgery ------------------------------------------

interface LsTreeEntry {
  mode: string;
  sha: string;
}

const NULL_SHA = "0000000000000000000000000000000000000000";

/** Load a commit's full tree as a path -> {mode, sha} map. One git call total. */
async function loadTreeEntries(
  commit: string,
): Promise<Map<string, LsTreeEntry>> {
  const out = await git("ls-tree", "-r", commit);
  const entries = new Map<string, LsTreeEntry>();
  for (const line of out.split("\n")) {
    if (!line) continue;
    const tab = line.indexOf("\t");
    if (tab < 0) continue;
    const parts = line.slice(0, tab).split(/\s+/);
    if (parts.length < 3) continue;
    entries.set(line.slice(tab + 1), { mode: parts[0]!, sha: parts[2]! });
  }
  return entries;
}

interface CommitMeta {
  authorName: string;
  authorEmail: string;
  authorDate: string;
  body: string;
  shortInfo: string;
}

/** Fetch all per-commit metadata in one git call, NUL-separated. */
async function loadCommitMeta(commit: string): Promise<CommitMeta> {
  const out = await git(
    "log",
    "-1",
    "--format=%an%x00%ae%x00%aI%x00%h %s%x00%B",
    commit,
  );
  const [authorName, authorEmail, authorDate, shortInfo, body] =
    out.split("\0");
  return {
    authorName: authorName ?? "",
    authorEmail: authorEmail ?? "",
    authorDate: authorDate ?? "",
    body: body ?? "",
    shortInfo: shortInfo ?? commit.slice(0, 7),
  };
}

/**
 * Build a new commit from `commit`'s tree, but with every generated path replaced by
 * its value at `scopeStart` (or removed if absent there). Returns the new commit sha,
 * or null if scrubbing left the tree identical to `newParent` (i.e. commit was
 * generated-only and should be dropped).
 *
 * Optimized: feeds all path updates to a single `git update-index --index-info` via stdin.
 */
async function rewriteCommit(
  commit: string,
  newParent: string,
  baseEntries: Map<string, LsTreeEntry>,
  generatedPaths: string[],
  parentTree: string,
  meta: CommitMeta,
): Promise<{ sha: string; tree: string } | null> {
  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "squash-gen-"));
  const tmpIndex = path.join(tmpDir, "index");
  const $$ = $({ env: { ...process.env, GIT_INDEX_FILE: tmpIndex } });

  try {
    await $$`git read-tree ${commit}`;

    const lines: string[] = [];
    for (const p of generatedPaths) {
      const baseEntry = baseEntries.get(p);
      if (baseEntry) {
        lines.push(`${baseEntry.mode} ${baseEntry.sha}\t${p}`);
      } else {
        lines.push(`0 ${NULL_SHA}\t${p}`);
      }
    }
    if (lines.length > 0) {
      await $({
        env: { ...process.env, GIT_INDEX_FILE: tmpIndex },
        input: lines.join("\n") + "\n",
      })`git update-index --index-info`;
    }

    const newTree = (await $$`git write-tree`).stdout.trim();
    if (newTree === parentTree) return null;

    const env = {
      ...process.env,
      GIT_AUTHOR_NAME: meta.authorName,
      GIT_AUTHOR_EMAIL: meta.authorEmail,
      GIT_AUTHOR_DATE: meta.authorDate,
    };
    const result = await $({
      env,
      input: meta.body,
    })`git commit-tree ${newTree} -p ${newParent}`;
    return { sha: result.stdout.trim(), tree: newTree };
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
}

async function rewriteScope(
  scopeStart: string,
  generatedPaths: string[],
): Promise<{ rewrote: number; dropped: number }> {
  const oldHead = await git("rev-parse", "HEAD");
  const commits = (await git("rev-list", "--reverse", `${scopeStart}..HEAD`))
    .split("\n")
    .filter(Boolean);

  // Precompute scope-start tree entries once (avoids ~N×P ls-tree spawns).
  const baseEntries = await loadTreeEntries(scopeStart);

  let newParent = scopeStart;
  let parentTree = await git("rev-parse", `${scopeStart}^{tree}`);
  let rewrote = 0;
  let dropped = 0;

  for (const commit of commits) {
    const meta = await loadCommitMeta(commit);
    const result = await rewriteCommit(
      commit,
      newParent,
      baseEntries,
      generatedPaths,
      parentTree,
      meta,
    );
    if (result === null) {
      dropped++;
      info(`  drop (empty after scrub): ${meta.shortInfo}`);
    } else {
      newParent = result.sha;
      parentTree = result.tree;
      rewrote++;
      info(`  keep: ${newParent.slice(0, 9)} <- ${meta.shortInfo}`);
    }
  }

  await git("update-ref", "HEAD", newParent, oldHead);
  await git("reset", "--hard", "HEAD");
  return { rewrote, dropped };
}

// ---------- regeneration -----------------------------------------------------

async function regenerateAll(): Promise<void> {
  info("  -> mise run gen:all");
  await $`mise run gen:all`;
}

// ---------- staging + commit -------------------------------------------------

/** Linguist-generated paths in the working tree (tracked + untracked, respecting .gitignore). */
async function generatedPathsInWorktree(): Promise<string[]> {
  const tracked = (await git("ls-files")).split("\n").filter(Boolean);
  const untracked = (await git("ls-files", "--others", "--exclude-standard"))
    .split("\n")
    .filter(Boolean);
  const generated = await filterLinguistGenerated([
    ...new Set([...tracked, ...untracked]),
  ]);
  return filterIgnored(generated);
}

/** Stage every generated path; return the count that actually has staged changes vs HEAD. */
async function stageGeneratedPaths(): Promise<number> {
  const paths = await generatedPathsInWorktree();
  if (paths.length === 0) return 0;
  await $`git add -- ${paths}`;
  const staged = await git("diff", "--cached", "--name-only");
  return staged.split("\n").filter(Boolean).length;
}

async function commitGenerated(): Promise<void> {
  const staged = await stageGeneratedPaths();
  if (staged === 0) {
    info("==> no linguist-generated changes to commit");
    return;
  }
  await $`git commit -m ${REGEN_SUBJECT}`;
  info(`==> ${REGEN_SUBJECT}`);
}

// ---------- modes ------------------------------------------------------------

async function runAmend(): Promise<void> {
  if (!(await isLabeledCommit("HEAD"))) {
    fail(
      `HEAD subject is not mig:/gen:/chore(gen): — refusing to fold gen artifacts into '${await subjectOf("HEAD")}'`,
    );
  }
  info("==> running gen:all");
  await $`mise run gen:all`;
  const staged = await stageGeneratedPaths();
  if (staged === 0) {
    info("no generated changes to amend");
    return;
  }
  await $`git commit --amend --no-edit`;
  info(`==> folded ${staged} generated path(s) into HEAD`);
}

async function runScopeRewrite(opts: Options): Promise<void> {
  const base = await resolveBase(opts);
  const head = await git("rev-parse", "HEAD");
  if (base === head) {
    info(
      `no commits between base (${base.slice(0, 7)}) and HEAD; nothing to do`,
    );
    return;
  }

  const scopeStart = await resolveScopeStart(base);
  const headIsScopeStart = scopeStart === head;

  if (!headIsScopeStart) await assertNoMergeCommits(scopeStart);

  const scopeCount = await git("rev-list", "--count", `${scopeStart}..HEAD`);
  info(
    `==> scope: ${scopeCount} commit(s) from ${scopeStart.slice(0, 7)} to ${head.slice(0, 7)}`,
  );

  if (headIsScopeStart && opts.drop) {
    info("HEAD is itself a labeled commit; nothing to drop");
    return;
  }

  const scopeCommits = headIsScopeStart
    ? [head]
    : (await git("rev-list", `${scopeStart}^..HEAD`))
        .split("\n")
        .filter(Boolean);
  const generatedPaths = await collectGeneratedPaths(scopeCommits);
  info(`==> tracking ${generatedPaths.length} generated path(s)`);

  if (!headIsScopeStart) {
    const { rewrote, dropped } = await rewriteScope(scopeStart, generatedPaths);
    info(`==> rewrote ${rewrote} commit(s), dropped ${dropped}`);
  }

  if (opts.drop) {
    info("--drop: skipping regeneration");
    return;
  }

  info("==> regenerating artifacts");
  await regenerateAll();
  await commitGenerated();
}

// ---------- entrypoint -------------------------------------------------------

async function main(): Promise<void> {
  const opts = loadOptions();
  await assertCleanWorktree();
  if (opts.amend) {
    await runAmend();
  } else {
    await runScopeRewrite(opts);
  }
}

main().catch((err) => {
  console.error("Fatal error:", err);
  process.exit(1);
});
