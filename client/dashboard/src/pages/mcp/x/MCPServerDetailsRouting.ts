import type { useRoutes } from "@/routes";

const VALID_TABS = [
  "overview",
  "inspect",
  "team-access",
  "sessions",
  "settings",
] as const;
const LEGACY_AUTHENTICATION_TAB = "authentication";
const LEGACY_TOOLS_TAB = "tools";

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

/** The Inspect tab was called Tools until AGE-2876; keep old links working. */
export function isLegacyToolsTabPath(
  pathname: string,
  mcpServerSlug: string,
): boolean {
  return tabSegmentFromPath(pathname, mcpServerSlug) === LEGACY_TOOLS_TAB;
}

export function initialTabFromHash(hash: string): TabValue {
  const hashValue = hash.replace("#", "");
  if (hashValue === LEGACY_AUTHENTICATION_TAB) return "settings";
  if (hashValue === LEGACY_TOOLS_TAB) return "inspect";
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
    case "inspect":
      return routes.mcp.x.inspect.href(mcpServerSlug);
    case "team-access":
      return routes.mcp.x.teamAccess.href(mcpServerSlug);
    case "sessions":
      return routes.mcp.x.sessions.href(mcpServerSlug);
    case "settings":
      return routes.mcp.x.settings.href(mcpServerSlug);
  }
}
