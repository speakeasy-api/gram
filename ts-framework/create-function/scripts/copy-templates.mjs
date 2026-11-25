// Copies the project templates from the speakeasy CLI, which embeds them, into
// this package so the published scaffolder ships the same templates.
import { cpSync, rmSync } from "node:fs";
import { basename, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const pkgDir = join(dirname(fileURLToPath(import.meta.url)), "..");
const source = join(
  pkgDir,
  "..",
  "..",
  "cli",
  "internal",
  "functions",
  "templates",
);
const target = join(pkgDir, "templates");
const skipped = new Set(["node_modules", "dist", ".git", "CHANGELOG.md"]);

rmSync(target, { recursive: true, force: true });
cpSync(source, target, {
  recursive: true,
  filter: (src) => !skipped.has(basename(src)),
});
