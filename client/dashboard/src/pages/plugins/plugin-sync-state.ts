import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";

/**
 * Whether one plugin's published package is current, from the project's
 * publish status. Freshness is per plugin: another plugin's pending changes,
 * such as a held change of a plugin whose auto sync is off, never make this
 * one read as needing a sync. Undefined means unknown (not connected, or a
 * connection that predates fingerprinting). A status without per-plugin
 * entries falls back to the project-wide answer.
 */
export function pluginUpToDate(
  publishStatus: PublishStatusResult | undefined,
  slug: string | undefined,
): boolean | undefined {
  if (!publishStatus?.connected) return undefined;
  if (!publishStatus.plugins || slug === undefined) {
    return publishStatus.upToDate;
  }
  return publishStatus.plugins.find((plugin) => plugin.slug === slug)?.upToDate;
}
