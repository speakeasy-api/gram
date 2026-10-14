import { execFile } from "node:child_process";
import { randomUUID } from "node:crypto";
import { basename, resolve } from "node:path";
import { promisify } from "node:util";

const exec = promisify(execFile);
const key = (directory) => `worktrees/${resolve(directory)}`;

async function run(file, args, cwd, signal) {
  const child = exec(file, args, { cwd, signal, maxBuffer: 16 * 1024 * 1024 });
  child.child.stdin.end();
  return child;
}

// Ownership is durable across plugin reloads, and independent of branch renames.
export function makeStrategy({
  directory: canonical,
  storage,
  OperationError,
}) {
  return {
    id: "speakeasy.worktrunk",
    async create({ sourceDirectory, directory, branch }, { signal }) {
      directory = resolve(directory);
      const name = `opencode/${basename(directory)}-${randomUUID().slice(0, 8)}`;
      // A literal Jinja expression preserves paths containing template syntax.
      const template = `{{ ${JSON.stringify(directory)} }}`;
      const { stdout } = await run(
        "wt",
        [
          "--config-set",
          `worktree-path=${JSON.stringify(template)}`,
          "switch",
          "--create",
          "--no-cd",
          "--yes",
          "--format=json",
          `--base=${branch ?? "HEAD"}`,
          name,
        ],
        sourceDirectory,
        signal,
      );
      const result = JSON.parse(stdout);
      if (
        typeof result.path !== "string" ||
        resolve(result.path) !== directory
      ) {
        throw new Error("Worktrunk returned an unexpected worktree path");
      }
      await storage.set(key(directory), true);
      return { directory };
    },
    async remove({ directory, force }, { signal }) {
      directory = resolve(directory);
      if ((await storage.get(key(directory))) !== true) {
        throw new Error("Worktree is not owned by the Worktrunk strategy");
      }
      // Check before cleanup hooks so a declined removal leaves services running.
      if (!force) {
        const { stdout } = await run(
          "git",
          ["status", "--porcelain", "--untracked-files=all"],
          directory,
          signal,
        );
        if (stdout.length) {
          throw new OperationError({
            message: "Worktree has uncommitted changes",
            forceRequired: true,
          });
        }
      }
      await run(
        "wt",
        [
          "remove",
          "--foreground",
          "--yes",
          "--format=json",
          ...(force ? ["--force"] : []),
          "--",
          directory,
        ],
        canonical,
        signal,
      );
      await storage.remove(key(directory));
    },
    async list(sourceDirectory, { signal }) {
      const { stdout } = await run(
        "wt",
        [
          "--config-set",
          "list.json-schema=2",
          "--config-set",
          "list.full=false",
          "list",
          "--format=json",
        ],
        sourceDirectory,
        signal,
      );
      const result = JSON.parse(stdout);
      if (result.schema !== 2 || !Array.isArray(result.items)) {
        throw new Error("Unsupported Worktrunk list JSON schema");
      }
      const inventory = [];
      for (const { worktree } of result.items) {
        if (!worktree?.path || worktree.prunable) continue;
        const directory = resolve(worktree.path);
        if (worktree.main) {
          inventory.push({ directory, type: "root" });
        } else if ((await storage.get(key(directory))) === true) {
          inventory.push({ directory, type: "worktree" });
        }
      }
      return inventory;
    },
  };
}
