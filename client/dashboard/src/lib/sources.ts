/**
 * Source type as used in deployment assets ("openapi", "function", "externalmcp").
 * URN kind as used in tool URNs ("http", "function", "externalmcp").
 *
 * The only non-trivial mapping is "openapi" ↔ "http".
 */

export type SourceType =
  | "openapi"
  | "function"
  | "externalmcp"
  | "remotemcp"
  | "tunneledmcp";
export type UrnKind =
  | "http"
  | "function"
  | "externalmcp"
  | "remotemcp"
  | "tunneledmcp";

const sourceTypeToUrn: Record<SourceType, UrnKind> = {
  openapi: "http",
  function: "function",
  externalmcp: "externalmcp",
  remotemcp: "remotemcp",
  tunneledmcp: "tunneledmcp",
};

const urnToSourceType: Record<UrnKind, SourceType> = {
  http: "openapi",
  function: "function",
  externalmcp: "externalmcp",
  remotemcp: "remotemcp",
  tunneledmcp: "tunneledmcp",
};

export function sourceTypeToUrnKind(type: SourceType): UrnKind {
  return sourceTypeToUrn[type];
}

export function urnKindToSourceType(kind: UrnKind): SourceType {
  return urnToSourceType[kind];
}

export function attachmentToURNPrefix(type: SourceType, slug: string): string {
  return `tools:${sourceTypeToUrnKind(type)}:${slug}:`;
}

export function formatRemoteMcpUrlForDisplay(url: string): string {
  return url.replace(/^https?:\/\//, "");
}

// formatRemoteMcpDisplay is the canonical "what to render for this server"
// helper: a non-empty user-supplied name wins, falling back to the
// protocol-stripped URL. Centralized so hero/breadcrumbs/cards/tables stay in
// sync.
export function formatRemoteMcpDisplay(server: {
  name?: string | null | undefined;
  url: string;
}): string {
  const trimmedName = server.name?.trim();
  if (trimmedName) {
    return trimmedName;
  }
  return formatRemoteMcpUrlForDisplay(server.url);
}

export function formatTunneledMcpDisplay(server: {
  name?: string | null | undefined;
}): string {
  return server.name?.trim() || "Tunneled MCP server";
}

// formatRemoteSessionIssuerDisplay picks the render-time primary label for a
// remote session issuer (remote identity provider): a non-empty display name
// wins, otherwise the protocol-stripped issuer URL. Centralized so the table,
// sheets, and any issuer cards stay in sync.
export function formatRemoteSessionIssuerDisplay(issuer: {
  name?: string | null | undefined;
  issuer: string;
}): string {
  return formatRemoteMcpDisplay({ name: issuer.name, url: issuer.issuer });
}

// A remote identity provider or session client belongs to one of three tenancy
// tiers, derived from which owning ids it carries. The API serializes an absent
// owner as an empty string, so a falsy check is the tier test.
//
// - project: owned by a single project (projectId set).
// - organization: shared across the org's projects (projectId empty,
//   organizationId set).
// - platform: the shared catalog curated by platform admins (both empty). Tenants
//   inherit these read-only and may attach their own clients, but cannot edit,
//   move, or delete them.
export type RemoteSessionScopeTier = "project" | "organization" | "platform";

export function remoteSessionScopeTier(entity: {
  projectId?: string | null;
  organizationId?: string | null;
}): RemoteSessionScopeTier {
  if (entity.projectId) return "project";
  if (entity.organizationId) return "organization";
  return "platform";
}

const REMOTE_SESSION_TIER_RANK: Record<RemoteSessionScopeTier, number> = {
  project: 0,
  organization: 1,
  platform: 2,
};

// resolveRemoteSessionIssuerByTierPrecedence picks the single entity a tenant
// should resolve to when several tiers expose a matching candidate (same slug or
// same upstream URL). Slugs are unique per project and, separately, across the
// platform catalog, but the organization tier has no slug uniqueness constraint,
// and any tier may point at the same URL, so callers must not rely on list order
// (which is by creation time and says nothing about tier). Returns the
// highest-priority candidate — project > organization > platform — or undefined
// when the list is empty.
export function resolveRemoteSessionIssuerByTierPrecedence<
  T extends { projectId?: string | null; organizationId?: string | null },
>(candidates: T[]): T | undefined {
  let best: T | undefined;
  let bestRank = Number.POSITIVE_INFINITY;
  for (const candidate of candidates) {
    const rank = REMOTE_SESSION_TIER_RANK[remoteSessionScopeTier(candidate)];
    if (rank < bestRank) {
      best = candidate;
      bestRank = rank;
    }
  }
  return best;
}

// Derive a default display name from an Issuer URL's hostname. Unlike the slug
// transform this keeps the hostname human-readable: dots are preserved rather
// than hyphenated. (URL parsing lowercases the host either way.) Returns null
// for empty or unparseable URLs so callers can leave the prior value intact
// while a partial URL is being typed.
export function deriveRemoteSessionIssuerNameFromUrl(
  url: string,
): string | null {
  const trimmed = url.trim();
  if (!trimmed) return null;
  try {
    const host = new URL(trimmed).hostname;
    return host || null;
  } catch {
    return null;
  }
}

// remoteMcpRouteParam returns the value to embed in dashboard URLs for a
// remote MCP server. Prefers the slug for human-friendly URLs and falls back
// to the ID; the server's getServer endpoint accepts either.
export function remoteMcpRouteParam(server: {
  id: string;
  slug?: string | null | undefined;
}): string {
  return server.slug?.trim() || server.id;
}

export function tunneledMcpRouteParam(server: { id: string }): string {
  return server.id;
}

// mcpServerRouteParam returns the value to embed in dashboard URLs for an
// mcp_server row. Mirrors remoteMcpRouteParam: prefers the slug for
// human-friendly URLs and falls back to the ID. The server's getMcpServer
// endpoint accepts a UUID id; route params that are UUID-shaped resolve
// directly, while non-UUID values are looked up by slug.
export function mcpServerRouteParam(server: {
  id: string;
  slug?: string | null | undefined;
}): string {
  return server.slug?.trim() || server.id;
}

// uuidRegex matches the canonical 8-4-4-4-12 hex form that Go's UUID package
// emits via String() and that the Goa-generated SDK uses on the wire. It is
// used to disambiguate route params produced by [remoteMcpRouteParam] and
// [mcpServerRouteParam].
const uuidRegex =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

// getRemoteMcpServerArgs maps a route-param string (which may be either an
// ID or a slug) into the request shape that [useGetRemoteMcpServer] consumes,
// where exactly one of `id` or `slug` must be supplied. UUID-shaped values
// are sent as `id`; everything else as `slug`.
export function getRemoteMcpServerArgs(idOrSlug: string): {
  id?: string;
  slug?: string;
} {
  if (uuidRegex.test(idOrSlug)) {
    return { id: idOrSlug };
  }
  return { slug: idOrSlug };
}

export function getTunneledMcpServerArgs(id: string): { id: string } {
  return { id };
}

// getMcpServerArgs maps a route-param string into the request shape that
// [useGetMcpServer] consumes. Mirrors [getRemoteMcpServerArgs] — UUID-shaped
// values resolve as `id`, everything else as `slug`.
export function getMcpServerArgs(idOrSlug: string): {
  id?: string;
  slug?: string;
} {
  if (uuidRegex.test(idOrSlug)) {
    return { id: idOrSlug };
  }
  return { slug: idOrSlug };
}
