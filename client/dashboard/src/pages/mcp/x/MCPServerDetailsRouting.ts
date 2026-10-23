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

function tabSegmentFromPath(
  pathname: string,
  mcpServerSlug: string,
): string | undefined {
  if (!mcpServerSlug) {
    return undefined;
  }

  const segments = pathname.split("/").filter(Boolean).map(decodePathSegment);
  const serverSlugIndex = segments.findIndex(
    (segment, index) =>
      segment === mcpServerSlug &&
      segments[index - 1] === "x" &&
      segments[index - 2] === "mcp",
  );

  if (serverSlugIndex === -1) {
    return undefined;
  }

  return segments[serverSlugIndex + 1];
}

export function activeTabFromPath(
  pathname: string,
  mcpServerSlug: string,
): TabValue | undefined {
  const tabSegment = tabSegmentFromPath(pathname, mcpServerSlug);
  return tabSegment && isValidTab(tabSegment) ? tabSegment : undefined;
}

export function isLegacyAuthenticationTabPath(
  pathname: string,
  mcpServerSlug: string,
): boolean {
  return (
    tabSegmentFromPath(pathname, mcpServerSlug) === LEGACY_AUTHENTICATION_TAB
  );
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
      return routes.mcp.x.overview.href(mcpServerSlug);
    case "tools":
      return routes.mcp.x.tools.href(mcpServerSlug);
    case "resources":
      return routes.mcp.x.resources.href(mcpServerSlug);
    case "prompts":
      return routes.mcp.x.prompts.href(mcpServerSlug);
    case "inspect":
      return routes.mcp.x.inspect.href(mcpServerSlug);
    case "team-access":
      return routes.mcp.x.teamAccess.href(mcpServerSlug);
    case "guardrails":
      return routes.mcp.x.guardrails.href(mcpServerSlug);
    case "sessions":
      return routes.mcp.x.sessions.href(mcpServerSlug);
    case "settings":
      return routes.mcp.x.settings.href(mcpServerSlug);
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
