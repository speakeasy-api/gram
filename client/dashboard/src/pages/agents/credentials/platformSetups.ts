import { claudeTagSetupInstructions } from "./ClaudeTagSetupInstructions";
import { GITHUB_ACTIONS_KEY } from "./githubActions";
import { githubActionsSetupInstructions } from "./GitHubActionsSetupInstructions";
import { gkeSetupInstructions } from "./GkeSetupInstructions";
import type { PlatformSetup } from "./setupInstructions";
import { isSupportedCatalogEntry, type CatalogEntry } from "./workloadCatalog";

/**
 * The catalog platforms this wizard ships a guided setup for, by the catalog's
 * own key.
 *
 * The picker and the setup it runs read this one map, so a platform cannot be
 * offered as a tile and then turn out to have nothing behind it.
 *
 * A Map rather than an object literal because the keys come from the catalog:
 * one called `constructor` or `toString` would be answered by
 * `Object.prototype` and offered as a tile with nothing behind it.
 */
const PLATFORM_SETUPS = new Map<string, (entry: CatalogEntry) => PlatformSetup>(
  [
    ["claude-tag", claudeTagSetupInstructions],
    [GITHUB_ACTIONS_KEY, githubActionsSetupInstructions],
    ["gke", gkeSetupInstructions],
  ],
);

/** Whether this catalog entry can use the guided setup this wizard ships. */
export function hasPlatformSetup(entry: CatalogEntry): boolean {
  return (
    entry.enabled &&
    isSupportedCatalogEntry(entry) &&
    PLATFORM_SETUPS.has(entry.key)
  );
}

/** A catalog platform's guided setup, or undefined where it ships none. */
export function platformSetup(entry: CatalogEntry): PlatformSetup | undefined {
  if (!hasPlatformSetup(entry)) return undefined;
  return PLATFORM_SETUPS.get(entry.key)?.(entry);
}
