import type { ExternalMCPServer } from "@gram/client/models/components/externalmcpserver.js";
import type { CatalogServer } from "../hooks";
import type { SetupType } from "../filter-defaults";
import type { FilterState } from "./useFilterState";

export interface ParsedServerMetadata {
  isLatest?: boolean;
  status?: string;
  authType: CatalogAuthType;
  authTypeDisplay: string;
  setupType: SetupType;
  publishedAt?: Date;
  updatedAt?: Date;
}

// Catalog responses may legitimately contain no optional metadata.
export function isCatalogServer(
  server: ExternalMCPServer,
): server is CatalogServer {
  return typeof server.registrySpecifier === "string";
}

function authSetup(server: CatalogServer) {
  return (
    server.authSetup ?? server.meta?.["com.speakeasy.ai/catalog"]?.authSetup
  );
}

/** Automatic OAuth setup still verifies the live issuer during installation. */
export function supportsAutomaticOAuth(server: CatalogServer): boolean {
  const metadata = server.meta?.["com.speakeasy.ai/catalog"];
  return (
    server.supportsDcr ||
    server.supportsCimd === true ||
    metadata?.supportsDcr === true ||
    metadata?.supportsCimd === true ||
    authSetup(server) === "dcr" ||
    authSetup(server) === "cimd"
  );
}

export function serverSetupType(server: CatalogServer): SetupType {
  const setup = authSetup(server);
  if (setup === "none" || setup === "dcr" || setup === "cimd") return "auto";
  if (setup === "manual") return "manual";
  return supportsAutomaticOAuth(server) ? "auto" : "unknown";
}

export function requiresManualSetup(server: CatalogServer): boolean {
  return serverSetupType(server) === "manual";
}

export type CatalogAuthType = "none" | "apikey" | "oauth" | "other" | "unknown";

export function extractAuthType(server: CatalogServer): CatalogAuthType {
  if (authSetup(server) === "none") return "none";
  if (authSetup(server) === "manual" || supportsAutomaticOAuth(server))
    return "oauth";
  return "unknown";
}

function suppliedDate(value: string | undefined): Date | undefined {
  if (!value) return undefined;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date;
}

export function parseServerMetadata(
  server: CatalogServer,
): ParsedServerMetadata {
  const lifecycle = server.meta?.["io.modelcontextprotocol.registry/official"];
  const authType = extractAuthType(server);
  return {
    isLatest: lifecycle?.isLatest,
    status: lifecycle?.status,
    authType,
    authTypeDisplay: {
      none: "No Auth",
      apikey: "API Key",
      oauth: "OAuth",
      other: "Other",
      unknown: "Unknown",
    }[authType],
    setupType: serverSetupType(server),
    publishedAt: suppliedDate(lifecycle?.publishedAt),
    updatedAt: suppliedDate(lifecycle?.updatedAt),
  };
}

/**
 * Check if a date is within a given range.
 */
function isWithinRange(date: Date | undefined, range: string): boolean {
  if (range === "any") return true;
  if (!date) return false;

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
 * Optionally applies a search query for client-side filtering.
 */
export function filterAndSortServers(
  servers: CatalogServer[],
  filterState: FilterState,
  searchQuery?: string,
): CatalogServer[] {
  // Parse metadata for all servers
  const serversWithMeta = servers.map((server) => ({
    server,
    metadata: parseServerMetadata(server),
    displayName: server.title ?? server.registrySpecifier,
  }));

  let filtered = serversWithMeta;

  // Apply search filter (client-side)
  if (searchQuery && searchQuery.trim() !== "") {
    const query = searchQuery.toLowerCase().trim();
    filtered = filtered.filter((s) => {
      const title = (s.server.title ?? "").toLowerCase();
      const specifier = s.server.registrySpecifier.toLowerCase();
      const description = (s.server.description ?? "").toLowerCase();
      return (
        title.includes(query) ||
        specifier.includes(query) ||
        description.includes(query)
      );
    });
  }

  // Apply granular filters
  const { filters } = filterState;

  // Auth type filter
  if (filters.authTypes.length > 0) {
    filtered = filtered.filter((s) =>
      filters.authTypes.includes(s.metadata.authType),
    );
  }

  // Unknown evidence must not match either confirmed setup path.
  if (filters.setupTypes.length > 0) {
    filtered = filtered.filter((s) =>
      filters.setupTypes.includes(s.metadata.setupType),
    );
  }

  // Updated range filter
  if (filters.updatedRange !== "any") {
    filtered = filtered.filter((s) =>
      isWithinRange(s.metadata.updatedAt, filters.updatedRange),
    );
  }

  // Apply sorting
  switch (filterState.sort) {
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
