import type { useRoutes } from "@/routes";

const VALID_TABS = [
  "overview",
  "inspect",
  "tools",
  "resources",
  "prompts",
  "authentication",
  "performance",
  "team-access",
  "settings",
] as const;

export type TabValue = (typeof VALID_TABS)[number];

export const MCP_SERVER_TAB_URLS: string[] = [...VALID_TABS];

// Tabs that only exist for a backend kind. Toolset-backed servers manage
// their tool bundle in place (tools/resources/prompts/authentication/
// performance); source-backed servers inspect their live upstream instead.
const TOOLSET_ONLY_TABS: readonly TabValue[] = [
  "tools",
  "resources",
  "prompts",
  "authentication",
  "performance",
];

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

export function initialTabFromHash(
  hash: string,
  isToolsetBacked: boolean,
): TabValue {
  const hashValue = hash.replace("#", "");
  if (!isValidTab(hashValue)) return "overview";
  return resolveTabForBackend(hashValue, isToolsetBacked).tab;
}

// resolveTabForBackend maps a requested tab onto one that exists for the
// server's backend kind, so old links keep working: source-backed servers
// fold the toolset-only tabs into their nearest surviving surface (the Tools
// tab was called Inspect before AGE-2876; authentication lives under
// Settings), while toolset-backed servers send Inspect to Tools.
export function resolveTabForBackend(
  tab: TabValue,
  isToolsetBacked: boolean,
): { tab: TabValue; hash?: string } {
  if (isToolsetBacked) {
    if (tab === "inspect") return { tab: "tools" };
    return { tab };
  }

  switch (tab) {
    case "tools":
      return { tab: "inspect" };
    case "authentication":
      return { tab: "settings", hash: "authentication" };
    case "resources":
    case "prompts":
    case "performance":
      return { tab: "overview" };
    case "overview":
    case "inspect":
    case "team-access":
    case "settings":
      return { tab };
  }
}

export function tabsForBackend(isToolsetBacked: boolean): readonly TabValue[] {
  if (isToolsetBacked) {
    return VALID_TABS.filter((tab) => tab !== "inspect");
  }
  return VALID_TABS.filter((tab) => !TOOLSET_ONLY_TABS.includes(tab));
}

export function mcpServerTabHref(
  routes: ReturnType<typeof useRoutes>,
  mcpServerSlug: string,
  tab: TabValue,
): string {
  switch (tab) {
    case "overview":
      return routes.mcp.x.overview.href(mcpServerSlug);
    case "inspect":
      return routes.mcp.x.inspect.href(mcpServerSlug);
    case "tools":
      return routes.mcp.x.tools.href(mcpServerSlug);
    case "resources":
      return routes.mcp.x.resources.href(mcpServerSlug);
    case "prompts":
      return routes.mcp.x.prompts.href(mcpServerSlug);
    case "authentication":
      return routes.mcp.x.authentication.href(mcpServerSlug);
    case "performance":
      return routes.mcp.x.performance.href(mcpServerSlug);
    case "team-access":
      return routes.mcp.x.teamAccess.href(mcpServerSlug);
    case "settings":
      return routes.mcp.x.settings.href(mcpServerSlug);
  }
}
