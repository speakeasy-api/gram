import type { useRoutes } from "@/routes";
import { createDetailRouting, decodePathSegment } from "../detailRouting";

// Project pages sit under /:orgSlug/projects/:projectSlug, so `mcp` is segment 3.
export const MCP_SECTION_INDEX = 3;

const routing = createDetailRouting({
  tabs: [
    "setup",
    "overview",
    "tools",
    "resources",
    "prompts",
    "inspect",
    "skills",
    "team-access",
    "guardrails",
    "sessions",
    "settings",
  ],
  idSegmentIndex: (segments, mcpServerSlug) =>
    segments[MCP_SECTION_INDEX] === "mcp" &&
    segments[MCP_SECTION_INDEX + 1] === mcpServerSlug
      ? MCP_SECTION_INDEX + 1
      : -1,
  tabRoutes: (routes) => ({
    setup: routes.mcp.details.setup,
    overview: routes.mcp.details.overview,
    tools: routes.mcp.details.tools,
    resources: routes.mcp.details.resources,
    prompts: routes.mcp.details.prompts,
    inspect: routes.mcp.details.inspect,
    skills: routes.mcp.details.skills,
    "team-access": routes.mcp.details.teamAccess,
    guardrails: routes.mcp.details.guardrails,
    sessions: routes.mcp.details.sessions,
    settings: routes.mcp.details.settings,
  }),
});

export const MCP_SERVER_TAB_URLS: string[] = routing.tabUrls;

export type TabValue = Parameters<typeof routing.tabHref>[2];

// Hosted servers manage their toolset on these tabs; every other server inspects its live tools.
const HOSTED_ONLY_TABS: readonly TabValue[] = ["tools", "resources", "prompts"];

function pathSegments(pathname: string): string[] {
  return pathname.split("/").filter(Boolean);
}

/** The segment after `/mcp/<slug>`, whether or not it names a server tab. */
export const tabSegmentFromPath = routing.tabSegmentFromPath;

/** The tab segment of `/toolsets/<toolsetSlug>/<tab>`. */
export function toolsetRouteTabSegment(pathname: string): string | undefined {
  const segments = pathSegments(pathname).map(decodePathSegment);
  if (segments[MCP_SECTION_INDEX] !== "toolsets") return undefined;
  return segments[MCP_SECTION_INDEX + 2];
}

/** `/mcp/x/<slug>/...` as `/mcp/<slug>/...`, or undefined for other paths. */
export function withoutMcpXSegment(pathname: string): string | undefined {
  const segments = pathSegments(pathname);
  if (
    segments[MCP_SECTION_INDEX] !== "mcp" ||
    decodePathSegment(segments[MCP_SECTION_INDEX + 1] ?? "") !== "x" ||
    segments.length <= MCP_SECTION_INDEX + 2
  ) {
    return undefined;
  }
  segments.splice(MCP_SECTION_INDEX + 1, 1);
  return `/${segments.join("/")}`;
}

export const activeTabFromPath = routing.activeTabFromPath;

export const mcpServerTabHref = routing.tabHref;

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
  if (hosted) return tab;
  if (tab === "tools") return "inspect";
  return HOSTED_ONLY_TABS.includes(tab) ? "overview" : tab;
}

/** Where to go after a save renamed the server's route slug, keeping tab, query and hash; undefined when unchanged. */
export function renamedServerHref(
  routes: ReturnType<typeof useRoutes>,
  currentParam: string,
  server: { id: string; slug?: string | null | undefined },
  location: { pathname: string; search: string; hash: string },
): string | undefined {
  const nextParam = server.slug?.trim() || server.id;
  if (
    !currentParam ||
    currentParam === nextParam ||
    currentParam === server.id
  ) {
    return undefined;
  }
  const tab = activeTabFromPath(location.pathname, currentParam) ?? "overview";
  return `${mcpServerTabHref(routes, nextParam, tab)}${location.search}${location.hash}`;
}
