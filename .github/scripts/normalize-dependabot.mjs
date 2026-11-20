import { execFileSync, spawnSync } from "node:child_process";
import {
  appendFileSync,
  copyFileSync,
  mkdirSync,
  readFileSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import { isDeepStrictEqual } from "node:util";

const workspace = "pnpm-workspace.yaml";
const lockfile = "pnpm-lock.yaml";
const files = [lockfile, workspace];
const git = (...args) => execFileSync("git", args, { encoding: "utf8" });

function report(message) {
  console.log(message);
  if (process.env.GITHUB_STEP_SUMMARY)
    appendFileSync(process.env.GITHUB_STEP_SUMMARY, `${message}\n`);
}

function output(name, value) {
  if (process.env.GITHUB_OUTPUT)
    appendFileSync(process.env.GITHUB_OUTPUT, `${name}=${value}\n`);
}

function checkScopeForNormalization(base) {
  const changes = git(
    "diff",
    "--raw",
    "--no-renames",
    "-z",
    `${base}...HEAD`,
    "--",
  ).split("\0");
  changes.pop(); // Git terminates the last path with a NUL.
  if (
    !changes.length ||
    changes.some((part, i) =>
      i % 2 === 0
        ? !/^:100644 100644 .* M$/.test(part)
        : !files.includes(part) && !/(^|\/)package\.json$/.test(part),
    )
  ) {
    report(
      "Skipped: changes are not limited to JavaScript manifests and the lockfile.",
    );
    return [];
  }
  const paths = changes.filter((_, i) => i % 2 === 1);
  if (paths.every((path) => path === lockfile)) {
    report(
      "Skipped: lockfile-only update. The normalizer cannot safely reproduce it from the manifests.",
    );
    return [];
  }
  return paths;
}
const stableVersion = /^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/;
const packageName = /^(?:@[a-z0-9_.-]+\/)?[a-z0-9_.-]+$/i;
const javascript = new Set(["npm_and_yarn", "npm", "pnpm"]);
const yq = (expression, args, options = {}) =>
  execFileSync("yq", [expression, ...args], { encoding: "utf8", ...options });
const escape = (str) => str.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

function readUpdates() {
  const updates = JSON.parse(process.env.DEPENDABOT_UPDATES || "[]");
  if (
    !updates.length ||
    !updates.every(
      ({ dependencyName, packageEcosystem }) =>
        typeof dependencyName === "string" &&
        packageName.test(dependencyName) &&
        javascript.has(packageEcosystem),
    )
  ) {
    throw new Error("No verified JavaScript update metadata.");
  }

  return updates;
}

function bumpOverrides(updates) {
  const parse = (path, options) =>
    JSON.parse(yq(".", ["-o=json", path], options));
  const before = parse(workspace);
  const overrides = before.overrides ?? {};
  const lines = readFileSync(workspace, "utf8").split(/(?<=\n)/);
  const edits = new Map();

  for (const [key, pin] of Object.entries(overrides)) {
    const matching = updates.filter(({ dependencyName }) =>
      new RegExp(`(?:^|>\\s*)${escape(dependencyName)}(?:@|$)`).test(key),
    );
    if (!matching.length) continue;
    const { dependencyName, prevVersion, newVersion } = matching[0];
    if (
      key !== dependencyName ||
      typeof pin !== "string" ||
      !stableVersion.test(pin) ||
      !stableVersion.test(newVersion) ||
      matching.some((m) => m.newVersion !== newVersion)
    ) {
      throw new Error(`Matching override needs review: ${key}.`);
    }
    if (pin === newVersion) continue;
    if (
      pin !== prevVersion ||
      matching.some((m) => m.prevVersion !== prevVersion) ||
      pin.split(".")[0] !== newVersion.split(".")[0]
    ) {
      throw new Error(
        `Cannot safely move override ${key} from ${pin} to ${newVersion}.`,
      );
    }

    // Use the parser's line location, but edit only the value so comments and formatting stay intact.
    const index =
      Number(
        yq(".overrides[strenv(OVERRIDE)] | line", [workspace], {
          env: { ...process.env, OVERRIDE: key },
        }).trim(),
      ) - 1;
    const pattern = new RegExp(
      `^([ \\t]+[^:\\n]+:[ \\t]*)(['"]?)${escape(pin)}\\2(?=[ \\t]*(?:#.*)?\\r?\\n?$)`,
    );
    if (!lines[index] || !pattern.test(lines[index])) {
      throw new Error(`Cannot preserve formatting for override ${key}.`);
    }
    lines[index] = lines[index].replace(
      pattern,
      (_, prefix, quote) => `${prefix}${quote}${newVersion}${quote}`,
    );
    edits.set(key, newVersion);
  }

  if (edits.size) {
    const updated = lines.join("");
    const expected = {
      ...before,
      overrides: { ...overrides, ...Object.fromEntries(edits) },
    };
    if (!isDeepStrictEqual(parse("-", { input: updated }), expected)) {
      throw new Error("Override edit changed unexpected workspace settings.");
    }
    writeFileSync(workspace, updated);
    for (const [name, version] of edits)
      report(`Bumped override ${name} to ${version}.`);
  }
  return edits.size > 0;
}

// Verify only the updated declarations, not unrelated snapshots or the dependency graph.
function requestedResolutions(base, head, paths, updates) {
  const mergeBase = git("merge-base", base, head).trim();
  const sections = ["dependencies", "devDependencies", "optionalDependencies"];
  const roots = [];
  for (const path of paths) {
    if (path === lockfile) continue;
    const read = (ref) => {
      const source = git("show", `${ref}:${path}`);
      return path === workspace
        ? JSON.parse(yq(".", ["-o=json", "-"], { input: source }))
        : JSON.parse(source);
    };
    const before = read(mergeBase);
    const after = read(head);
    for (const { dependencyName: name, newVersion: version } of updates) {
      const matches = (oldValue, newValue) =>
        typeof newValue === "string" &&
        oldValue !== newValue &&
        newValue.replace(/^[~^]/, "") === version;
      if (path === workspace) {
        if (matches(before.catalog?.[name], after.catalog?.[name])) {
          roots.push({
            name,
            version,
            location: "default catalog",
            lockPath: ["catalogs", "default", name],
          });
        }
      } else {
        const importer =
          path === "package.json"
            ? "."
            : path.slice(0, -"/package.json".length);
        for (const section of sections) {
          if (matches(before[section]?.[name], after[section]?.[name])) {
            roots.push({
              name,
              version,
              location: importer,
              lockPath: ["importers", importer, section, name],
            });
          }
        }
      }
    }
  }
  for (const { dependencyName: name, newVersion: version } of updates) {
    if (
      !stableVersion.test(version) ||
      !roots.some((root) => root.name === name && root.version === version)
    ) {
      throw new Error(
        `Cannot confirm a manifest or catalog bump for ${name} to ${version}. Nothing was pushed.`,
      );
    }
  }
  return roots;
}

function verifyResolutions(roots) {
  const lock = JSON.parse(
    yq('{"importers": .importers, "catalogs": .catalogs}', [
      "-o=json",
      lockfile,
    ]),
  );
  for (const { name, version, location, lockPath } of roots) {
    const resolved = lockPath
      .reduce((data, key) => data?.[key], lock)
      ?.version?.split("(", 1)[0];
    if (resolved !== version) {
      throw new Error(
        `Expected ${name} ${version} in ${location}; aube resolved ${resolved ?? "nothing"}. Nothing was pushed.`,
      );
    }
  }
}

function normalize(base, head, out) {
  // Recheck before touching files, including when this command is run locally.
  if (
    git("rev-parse", "HEAD").trim() !== head ||
    git("status", "--porcelain")
  ) {
    throw new Error("Start from a clean checkout of the expected PR head.");
  }
  const paths = checkScopeForNormalization(base);
  if (!paths.length) return;
  const updates = readUpdates();
  const roots = requestedResolutions(base, head, paths, updates);

  let edited;
  try {
    edited = bumpOverrides(updates);
  } catch (error) {
    throw new Error(
      `Override update needs review. Nothing was pushed. ${error.message}`,
      { cause: error },
    );
  }
  if (edited) {
    // The normalization task requires a clean tree. This scratch commit is never pushed.
    git("add", "--", workspace);
    git("diff", "--cached", "--check");
    git(
      "-c",
      "core.hooksPath=/dev/null",
      "-c",
      "user.name=gram-bot[bot]",
      "-c",
      "user.email=gram-bot[bot]@users.noreply.github.com",
      "commit",
      "-m",
      "chore(deps): normalize scratch overrides",
    );
  }

  try {
    execFileSync("mise", ["run", "--skip-tools", "deps:normalize-lockfile"], {
      stdio: "inherit",
    });
  } catch (error) {
    throw new Error(
      "Lockfile normalization failed. See the step log. Nothing was pushed.",
      { cause: error },
    );
  }

  verifyResolutions(roots);

  const changed = files.filter((file) => {
    const { status, error } = spawnSync(
      "git",
      ["diff", "--quiet", head, "--", file],
      { stdio: "inherit" },
    );
    if (error || (status !== 0 && status !== 1))
      throw new Error(`Cannot compare ${file} to the PR head.`);
    return status === 1;
  });
  if (!changed.length) {
    report("The lockfile is already normalized.");
    return;
  }
  mkdirSync(out);
  for (const file of changed) copyFileSync(file, join(out, file));
  output("changed", "true");
}

try {
  const [command, base, head, out] = process.argv.slice(2);
  if (command === "scope" && base && !head) {
    if (checkScopeForNormalization(base).length) output("eligible", "true");
  } else if (command === "normalize" && base && head && out) {
    normalize(base, head, out);
  } else {
    throw new Error(
      "Usage: normalize-dependabot.mjs scope <base-sha> | normalize <base-sha> <head-sha> <artifact-dir>",
    );
  }
} catch (error) {
  report(error.message);
  process.exitCode = 1;
}
