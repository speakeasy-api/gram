import { shadowMCPCatalogMatch } from "@/components/shadow-mcp/shadowMCPOfficialServer";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import type { CatalogServer } from "./hooks";

/** How much the team already uses a catalog server on its own. */
export type CatalogTeamUsage = {
  /** People who called it. The most seen on any one of its URLs. */
  userCount: number;
  /** Calls across all of its URLs. */
  callCount: number;
};

/** Catalog entries keyed by `registrySpecifier`. */
export type CatalogTeamUsageMap = ReadonlyMap<string, CatalogTeamUsage>;

/**
 * Joins the org's Shadow MCP inventory to the catalog: for each entry, the
 * usage seen on the URLs it serves. URLs are compared the way Shadow MCP
 * compares them (case, default port and trailing slash do not matter).
 * Local commands carry no URL, so they never match.
 */
export function catalogTeamUsage(
  catalog: readonly CatalogServer[],
  inventory: readonly ShadowMCPInventoryServer[],
): Map<string, CatalogTeamUsage> {
  const usage = new Map<string, CatalogTeamUsage>();
  for (const server of inventory) {
    if (server.targetKind === "stdio_command") continue;
    if (server.observedUseCount <= 0 && server.userCount <= 0) continue;
    // Several entries can serve the same URL (a vendor listed twice); each
    // of them gets the usage.
    for (const entry of catalog) {
      if (!shadowMCPCatalogMatch([entry], server.canonicalServerUrl)) continue;
      const seen = usage.get(entry.registrySpecifier);
      // One person can call several of an entry's URLs, so people are not
      // added up across URLs; calls are.
      usage.set(entry.registrySpecifier, {
        userCount: Math.max(seen?.userCount ?? 0, server.userCount),
        callCount: (seen?.callCount ?? 0) + server.observedUseCount,
      });
    }
  }
  return usage;
}

/** "Used by 5 people · 51 calls". */
export function teamUsageLabel(usage: CatalogTeamUsage): string {
  const people = `${usage.userCount.toLocaleString()} ${usage.userCount === 1 ? "person" : "people"}`;
  const calls = `${usage.callCount.toLocaleString()} ${usage.callCount === 1 ? "call" : "calls"}`;
  return `Used by ${people} · ${calls}`;
}

/** Shown on hover, so the counts say where they come from. */
export const TEAM_USAGE_TOOLTIP =
  "Your team's agents connect to this server directly. Counts cover every call Shadow MCP has seen.";
