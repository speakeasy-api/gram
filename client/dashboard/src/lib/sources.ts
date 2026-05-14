/**
 * Source type as used in deployment assets ("openapi", "function", "externalmcp").
 * URN kind as used in tool URNs ("http", "function", "externalmcp").
 *
 * The only non-trivial mapping is "openapi" ↔ "http".
 */

export type SourceType = "openapi" | "function" | "externalmcp" | "remotemcp";
export type UrnKind = "http" | "function" | "externalmcp" | "remotemcp";

const sourceTypeToUrn: Record<SourceType, UrnKind> = {
  openapi: "http",
  function: "function",
  externalmcp: "externalmcp",
  remotemcp: "remotemcp",
};

const urnToSourceType: Record<UrnKind, SourceType> = {
  http: "openapi",
  function: "function",
  externalmcp: "externalmcp",
  remotemcp: "remotemcp",
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

// remoteMcpRouteParam returns the value to embed in dashboard URLs for a
// remote MCP server. Prefers the slug for human-friendly URLs and falls back
// to the ID; the server's getServer endpoint accepts either.
export function remoteMcpRouteParam(server: {
  id: string;
  slug?: string | null | undefined;
}): string {
  return server.slug?.trim() || server.id;
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
