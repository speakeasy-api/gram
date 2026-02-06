import type { Server } from "../hooks";
import type { FilterState } from "./useFilterState";

interface ToolAnnotations {
  title?: string;
  readOnlyHint?: boolean;
  destructiveHint?: boolean;
}

interface ToolInfo {
  name: string;
  description?: string;
  annotations?: ToolAnnotations;
}

/**
 * Parsed metadata from a server for display and filtering.
 */
export interface ParsedServerMetadata {
  // Usage stats
  visitorsWeek: number;
  visitorsMonth: number;
  visitorsTotal: number;
  /** Estimated weekly breakdown for sparkline (4 values, oldest to newest) */
  weeklyData: number[];

  // Status
  isOfficial: boolean;
  isLatest: boolean;
  status?: string;

  // Auth
  authType: string;
  authTypeDisplay: string;

  // Tools
  toolCount: number;
  isReadOnly: boolean;

  // Dates
  publishedAt?: Date;
  updatedAt?: Date;
}

/**
 * Extract the authentication type from server metadata.
 */
function extractAuthType(server: Server): string {
  const versionMeta = server.meta["com.pulsemcp/server-version"];
  const remote = versionMeta?.["remotes[0]"];
  const authInfo = remote?.auth;

  if (!authInfo || !authInfo.type) {
    return "none";
  }

  const authTypeLower = authInfo.type.toLowerCase();

  if (authTypeLower === "none" || authTypeLower === "") {
    return "none";
  }
  if (authTypeLower.includes("oauth")) {
    return "oauth";
  }
  if (authTypeLower.includes("api") || authTypeLower.includes("key")) {
    return "apikey";
  }
  return "other";
}

/**
 * Get a human-readable display string for auth type.
 */
function getAuthTypeDisplay(authType: string): string {
  switch (authType) {
    case "none":
      return "No Auth";
    case "apikey":
      return "API Key";
    case "oauth":
      return "OAuth";
    default:
      return "Auth Required";
  }
}

/**
 * Extract tool-related metadata.
 */
function extractToolMetadata(server: Server): {
  toolCount: number;
  isReadOnly: boolean;
} {
  const versionMeta = server.meta["com.pulsemcp/server-version"];
  const remote = versionMeta?.["remotes[0]"];
  const metaTools = remote?.tools ?? [];
  const serverTools: ToolInfo[] = (server.tools ?? []) as ToolInfo[];
  const tools: ToolInfo[] = metaTools.length > 0 ? metaTools : serverTools;

  const toolCount = tools.length;
  const isReadOnly =
    toolCount > 0 &&
    tools.every((tool) => tool.annotations?.readOnlyHint === true);

  return { toolCount, isReadOnly };
}

/**
 * Estimate weekly breakdown from monthly and weekly totals.
 * Returns 4 values representing the last 4 weeks (oldest to newest).
 */
function estimateWeeklyData(
  visitorsWeek: number,
  visitorsMonth: number,
): number[] {
  if (visitorsMonth === 0) {
    return [0, 0, 0, 0];
  }

  // The most recent week is known
  const recentWeek = visitorsWeek;
  // Remaining visitors distributed across the other 3 weeks
  const remaining = Math.max(0, visitorsMonth - recentWeek);
  const avgOther = Math.round(remaining / 3);

  // Add slight variation for visual interest (but keep it realistic)
  const variance = Math.round(avgOther * 0.15);
  return [
    Math.max(0, avgOther - variance),
    avgOther,
    Math.max(0, avgOther + variance),
    recentWeek,
  ];
}

/**
 * Parse all relevant metadata from a server.
 */
export function parseServerMetadata(server: Server): ParsedServerMetadata {
  const serverMeta = server.meta["com.pulsemcp/server"];
  const versionMeta = server.meta["com.pulsemcp/server-version"];

  const visitorsWeek = serverMeta?.visitorsEstimateMostRecentWeek ?? 0;
  const visitorsMonth = serverMeta?.visitorsEstimateLastFourWeeks ?? 0;
  const visitorsTotal = serverMeta?.visitorsEstimateTotal ?? 0;

  const authType = extractAuthType(server);
  const { toolCount, isReadOnly } = extractToolMetadata(server);

  return {
    visitorsWeek,
    visitorsMonth,
    visitorsTotal,
    weeklyData: estimateWeeklyData(visitorsWeek, visitorsMonth),
    isOfficial: serverMeta?.isOfficial ?? false,
    isLatest: versionMeta?.isLatest ?? false,
    status: versionMeta?.status,
    authType,
    authTypeDisplay: getAuthTypeDisplay(authType),
    toolCount,
    isReadOnly,
    publishedAt: versionMeta?.publishedAt
      ? new Date(versionMeta.publishedAt)
      : undefined,
    updatedAt: versionMeta?.updatedAt
      ? new Date(versionMeta.updatedAt)
      : undefined,
  };
}

/**
 * Check if a date is within a given range.
 */
function isWithinRange(date: Date | undefined, range: string): boolean {
  if (!date || range === "any") return true;

  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffDays = diffMs / (1000 * 60 * 60 * 24);

  switch (range) {
    case "week":
      return diffDays <= 7;
    case "month":
      return diffDays <= 30;
    case "year":
      return diffDays <= 365;
    default:
      return true;
  }
}

/**
 * Filter and sort servers based on the current filter state.
 */
export function filterAndSortServers(
  servers: Server[],
  filterState: FilterState,
): Server[] {
  // Parse metadata for all servers
  const serversWithMeta = servers.map((server) => ({
    server,
    metadata: parseServerMetadata(server),
    displayName: server.title ?? server.registrySpecifier,
  }));

  let filtered = serversWithMeta;

  // Apply category filter
  if (filterState.category === "popular") {
    filtered = filtered.filter((s) => s.metadata.visitorsMonth >= 100);
  }
  // "all" - no filter

  // Apply granular filters
  const { filters } = filterState;

  // Auth type filter
  if (filters.authTypes.length > 0) {
    filtered = filtered.filter((s) =>
      filters.authTypes.includes(
        s.metadata.authType as "none" | "apikey" | "oauth" | "other",
      ),
    );
  }

  // Tool behavior filter
  if (filters.toolBehaviors.length > 0) {
    filtered = filtered.filter((s) => {
      if (filters.toolBehaviors.includes("readonly") && s.metadata.isReadOnly)
        return true;
      if (filters.toolBehaviors.includes("write") && !s.metadata.isReadOnly)
        return true;
      return false;
    });
  }

  // Popularity filter
  if (filters.minUsers > 0) {
    filtered = filtered.filter(
      (s) => s.metadata.visitorsMonth >= filters.minUsers,
    );
  }

  // Updated range filter
  if (filters.updatedRange !== "any") {
    filtered = filtered.filter((s) =>
      isWithinRange(s.metadata.updatedAt, filters.updatedRange),
    );
  }

  // Tool count filter
  if (filters.minTools > 0) {
    filtered = filtered.filter((s) => s.metadata.toolCount >= filters.minTools);
  }

  // Apply sorting
  switch (filterState.sort) {
    case "popular":
      filtered = filtered.sort(
        (a, b) => b.metadata.visitorsMonth - a.metadata.visitorsMonth,
      );
      break;
    case "recent":
      filtered = filtered.sort((a, b) => {
        const dateA = a.metadata.publishedAt?.getTime() ?? 0;
        const dateB = b.metadata.publishedAt?.getTime() ?? 0;
        return dateB - dateA;
      });
      break;
    case "updated":
      filtered = filtered.sort((a, b) => {
        const dateA = a.metadata.updatedAt?.getTime() ?? 0;
        const dateB = b.metadata.updatedAt?.getTime() ?? 0;
        return dateB - dateA;
      });
      break;
    case "alphabetical":
      filtered = filtered.sort((a, b) =>
        a.displayName.localeCompare(b.displayName),
      );
      break;
    case "alphabetical-desc":
      filtered = filtered.sort((a, b) =>
        b.displayName.localeCompare(a.displayName),
      );
      break;
  }

  return filtered.map((s) => s.server);
}

/**
 * Count servers matching each category for display badges.
 */
export function countByCategory(
  servers: Server[],
): Record<"all" | "popular", number> {
  const serversWithMeta = servers.map((server) => parseServerMetadata(server));

  return {
    all: servers.length,
    popular: serversWithMeta.filter((m) => m.visitorsMonth >= 100).length,
  };
}
