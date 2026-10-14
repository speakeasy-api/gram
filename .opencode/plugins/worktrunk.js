import { isPluginEnabled } from "../lib/plugins.js";
import { makeStrategy } from "../lib/worktrunk.js";

const pluginID = "speakeasy.worktrunk";

/** @type {import("@opencode/plugin").Plugin.Plugin} */
export default {
  id: pluginID,
  async setup(ctx) {
    if (!(await isPluginEnabled(ctx.location.directory, pluginID))) return;

    const { Worktree } = await import("@opencode/plugin");
    const strategy = makeStrategy({
      directory: ctx.location.project.canonical,
      storage: ctx.storage,
      OperationError: Worktree.OperationError,
    });
    await ctx.worktree.transform((editor) => editor.add(strategy));
  },
};
