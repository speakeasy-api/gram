import { execFile } from "node:child_process";
import { promisify } from "node:util";

const exec = promisify(execFile);
const pluginID = "speakeasy.pause-stack";

/** @type {import("@opencode/plugin").Plugin.Plugin} */
export default {
  id: pluginID,
  async setup(ctx) {
    try {
      const { stdout } = await exec("mise", ["set", "GRAM_OPENCODE_PLUGINS"], {
        cwd: ctx.location.directory,
      });
      if (!stdout.split(",").some((id) => id.trim() === pluginID)) return;
    } catch {
      // An unset variable (or failed lookup) leaves the plugin disabled.
      return;
    }

    const controller = new AbortController();

    /** @param {Parameters<typeof ctx.session.get>[0]["sessionID"]} sessionID */
    async function pause(sessionID) {
      try {
        const session = await ctx.session.get({ sessionID });
        // Child agents finish inside a parent's turn. Events are server-wide,
        // and a session may have moved since its idle event was emitted.
        if (
          controller.signal.aborted ||
          session.parentID ||
          session.location.directory !== ctx.location.directory
        ) {
          return;
        }

        await exec("mise", ["run", "pause"], {
          cwd: session.location.directory,
        });
      } catch (error) {
        console.error(
          "[speakeasy.pause-stack] Failed to pause the stack:",
          error,
        );
      }
    }

    void (async () => {
      for await (const event of ctx.event.subscribe({
        signal: controller.signal,
      })) {
        if (
          event.type === "session.status" &&
          event.data.status.type === "idle" &&
          event.location?.directory === ctx.location.directory &&
          event.location?.workspaceID === ctx.location.workspaceID
        ) {
          await pause(event.data.sessionID);
        }
      }
    })().catch((error) => {
      if (!controller.signal.aborted) {
        console.error(
          "[speakeasy.pause-stack] Event subscription failed:",
          error,
        );
      }
    });

    // Let an already-started pause finish so its stack lock is released cleanly.
    return () => controller.abort();
  },
};
