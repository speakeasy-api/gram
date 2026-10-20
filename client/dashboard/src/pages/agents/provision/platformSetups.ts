import { claudeTagSetupInstructions } from "./ClaudeTagSetupInstructions";
import type { PlatformSetup } from "./setupInstructions";
import type { CatalogEntry } from "./workloadCatalog";

/**
 * The catalog platforms this wizard ships a guided setup for, by the catalog's
 * own key.
 *
 * The picker and the setup it runs read this one map, so a platform cannot be
 * offered as a tile and then turn out to have nothing behind it.
 */
const PLATFORM_SETUPS: Partial<
  Record<string, (entry: CatalogEntry) => PlatformSetup>
> = {
  "claude-tag": claudeTagSetupInstructions,
};

/** Whether this wizard ships a guided setup for a catalog platform. */
export function hasPlatformSetup(key: string): boolean {
  return PLATFORM_SETUPS[key] !== undefined;
}

/** A catalog platform's guided setup, or undefined where it ships none. */
export function platformSetup(entry: CatalogEntry): PlatformSetup | undefined {
  return PLATFORM_SETUPS[entry.key]?.(entry);
}
