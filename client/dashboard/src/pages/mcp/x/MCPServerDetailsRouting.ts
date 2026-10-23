import type { useRoutes } from "@/routes";

const VALID_TABS = [
  "overview",
  "tools",
  "resources",
  "prompts",
  "inspect",
  "team-access",
  "guardrails",
  "sessions",
  "settings",
] as const;
const LEGACY_AUTHENTICATION_TAB = "authentication";
// Hosted servers manage their toolset on these tabs; every other server inspects its live tools.
const HOSTED_ONLY_TABS: readonly TabValue[] = ["tools", "resources", "prompts"];

// Breadcrumb `skipSegments` source. Derived from VALID_TABS rather than
// hand-maintained alongside it, so adding a tab can't leave a stray crumb.
export const MCP_SERVER_TAB_URLS: string[] = [...VALID_TABS];

export type TabValue = (typeof VALID_TABS)[number];

function isValidTab(value: string): value is TabValue {
  return (VALID_TABS as readonly string[]).includes(value);
}

function decodePathSegment(segment: string): string {
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

// Project pages live at /:orgSlug/projects/:projectSlug/..., so the MCP
// section is always the fourth segment. Reading by position, not by searching
// for "mcp", keeps an org, project or server slugged "mcp" or "x" unambiguous.
const MCP_SECTION_INDEX = 3;

function pathSegments(pathname: string): string[] {
  return pathname.split("/").filter(Boolean);
}

/** The segment after `/mcp/<slug>`, whether or not it names a server tab. */
export function tabSegmentFromPath(
  pathname: string,
  mcpServerSlug: string,
): string | undefined {
  if (!mcpServerSlug) {
    return undefined;
  }

  const segments = pathSegments(pathname).map(decodePathSegment);
  if (
    segments[MCP_SECTION_INDEX] !== "mcp" ||
    segments[MCP_SECTION_INDEX + 1] !== mcpServerSlug
  ) {
    return undefined;
  }

  return segments[MCP_SECTION_INDEX + 2];
}

/**
 * The same path without the retired `x` segment: `/mcp/x/<slug>/<tab>` becomes
 * `/mcp/<slug>/<tab>`. Undefined when the path has no `/mcp/x/` prefix.
 */
export function withoutMcpXSegment(pathname: string): string | undefined {
  const segments = pathSegments(pathname);
  if (
    segments[MCP_SECTION_INDEX] !== "mcp" ||
    segments[MCP_SECTION_INDEX + 1] !== "x" ||
    segments.length <= MCP_SECTION_INDEX + 2
  ) {
    return undefined;
  }
  segments.splice(MCP_SECTION_INDEX + 1, 1);
  return `/${segments.join("/")}`;
}

export function activeTabFromPath(
  pathname: string,
  mcpServerSlug: string,
): TabValue | undefined {
  const tabSegment = tabSegmentFromPath(pathname, mcpServerSlug);
  return tabSegment && isValidTab(tabSegment) ? tabSegment : undefined;
}

export function initialTabFromHash(hash: string): TabValue {
  const hashValue = hash.replace("#", "");
  if (hashValue === LEGACY_AUTHENTICATION_TAB) return "settings";
  if (!isValidTab(hashValue)) return "overview";
  return hashValue;
}

export function mcpServerTabHref(
  routes: ReturnType<typeof useRoutes>,
  mcpServerSlug: string,
  tab: TabValue,
): string {
  switch (tab) {
    case "overview":
      return routes.mcp.details.overview.href(mcpServerSlug);
    case "tools":
      return routes.mcp.details.tools.href(mcpServerSlug);
    case "resources":
      return routes.mcp.details.resources.href(mcpServerSlug);
    case "prompts":
      return routes.mcp.details.prompts.href(mcpServerSlug);
    case "inspect":
      return routes.mcp.details.inspect.href(mcpServerSlug);
    case "team-access":
      return routes.mcp.details.teamAccess.href(mcpServerSlug);
    case "guardrails":
      return routes.mcp.details.guardrails.href(mcpServerSlug);
    case "sessions":
      return routes.mcp.details.sessions.href(mcpServerSlug);
    case "settings":
      return routes.mcp.details.settings.href(mcpServerSlug);
  }
}

// A hosted toolset's canonical wrapper shares the toolset's id; toolset writes sync onto it.
export function isCanonicalHostedWrapper(server: {
  id: string;
  toolsetId?: string | undefined;
}): boolean {
  return server.toolsetId !== undefined && server.id === server.toolsetId;
}

/** The canonical wrapper, or the server the API names as host of a toolset without one. */
export function standsInForToolset(server: {
  id: string;
  toolsetId?: string | undefined;
  toolset?: { hostingMcpServerId?: string | undefined } | undefined;
}): boolean {
  if (server.toolsetId === undefined) return false;
  const hostingId = server.toolset?.hostingMcpServerId;
  // Older servers omit the field; the canonical rule still holds there.
  return hostingId === undefined
    ? isCanonicalHostedWrapper(server)
    : hostingId === server.id;
}

/** The tab to show instead of `tab` for this kind of server, or `tab` itself. */
export function tabForServer(tab: TabValue, hosted: boolean): TabValue {
  if (hosted) return tab === "inspect" ? "tools" : tab;
  if (tab === "tools") return "inspect";
  return HOSTED_ONLY_TABS.includes(tab) ? "overview" : tab;
}
