import { execFile } from "node:child_process";
import { promisify } from "node:util";

/**
 * @param {string} directory
 * @param {string} pluginID
 * @returns {Promise<boolean>}
 */
export async function isPluginEnabled(directory, pluginID) {
  try {
    const { stdout } = await promisify(execFile)("mise", ["env", "--json"], {
      cwd: directory,
    });
    const plugins = JSON.parse(stdout).GRAM_OPENCODE_PLUGINS ?? "";
    return plugins.split(":").some((id) => id.trim() === pluginID);
  } catch {
    // An unset variable (or failed lookup) leaves the plugin disabled.
    return false;
  }
}
