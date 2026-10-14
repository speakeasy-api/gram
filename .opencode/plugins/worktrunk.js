import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { makeStrategy } from "../lib/worktrunk.js";

const exec = promisify(execFile);
const pluginID = "speakeasy.worktrunk";

/** @type {import("@opencode/plugin").Plugin.Plugin} */
export default {
  id: pluginID,
  async setup(ctx) {
    try {
      const { stdout } = await exec("mise", ["set", "GRAM_OPENCODE_PLUGINS"], {
        cwd: ctx.location.directory,
      });
      if (!stdout.split(":").some((id) => id.trim() === pluginID)) return;
    } catch {
      return;
    }

    const { Worktree } = await import("@opencode/plugin");
    const strategy = makeStrategy({
      directory: ctx.location.project.canonical,
      storage: ctx.storage,
      OperationError: Worktree.OperationError,
    });
    await ctx.worktree.transform((editor) => editor.add(strategy));
  },
};
