import { execFile } from "node:child_process";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { promisify } from "node:util";
import { isPluginEnabled } from "../lib/plugins.js";

const exec = promisify(execFile);
const pluginID = "speakeasy.pause-stack";

/** @param {string} path */
async function readMarker(path) {
  try {
    return await readFile(path, "utf8");
  } catch (error) {
    if (error.code === "ENOENT") return undefined;
    throw error;
  }
}

/** @param {string} directory */
async function gitDirectory(directory) {
  const { stdout } = await exec("git", ["rev-parse", "--absolute-git-dir"], {
    cwd: directory,
  });
  return stdout.trim();
}

/** @param {string} gitdir */
async function stackPausedOrBooting(gitdir) {
  if ((await readMarker(join(gitdir, "gram-stack-paused"))) !== undefined) {
    return true;
  }

  const pid = (await readMarker(join(gitdir, "gram-stack-boot.pid")))?.trim();
  if (!pid || !/^[1-9]\d*$/.test(pid)) return false;
  try {
    const { stdout } = await exec("ps", ["-o", "command=", "-p", pid]);
    // Like idle-pause, ignore stale markers and PIDs reused by other processes.
    return stdout.includes("workboot");
  } catch (error) {
    if (error.code === 1) return false; // The boot process has exited.
    throw error;
  }
}

/** @type {import("@opencode/plugin").Plugin.Plugin} */
export default {
  id: pluginID,
  async setup(ctx) {
    if (!(await isPluginEnabled(ctx.location.directory, pluginID))) return;

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

        const gitdir = await gitDirectory(session.location.directory);
        if (await stackPausedOrBooting(gitdir)) return;

        // The plugin context does not expose session.active. Query the local
        // server through the CLI so sessions active before plugin load count too.
        const { stdout } = await exec(
          "opencode",
          ["api", "get", "/api/session/active"],
          {
            cwd: session.location.directory,
          },
        );
        const active = JSON.parse(stdout).data;
        for (const activeID of Object.keys(active)) {
          // The ending session can still own its drain when idle is published.
          if (activeID === sessionID) continue;
          const other = await ctx.session.get({ sessionID: activeID });
          if (
            !other.parentID &&
            other.projectID === session.projectID &&
            other.location.workspaceID === ctx.location.workspaceID &&
            (await gitDirectory(other.location.directory)) === gitdir
          ) {
            return;
          }
        }
        if (controller.signal.aborted) return;

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
