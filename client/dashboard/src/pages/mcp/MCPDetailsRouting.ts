import type { useRoutes } from "@/routes";

const VALID_TABS = [
  "overview",
  "tools",
  "resources",
  "prompts",
  "authentication",
  "performance",
  "team-access",
  "settings",
] as const;

export const MCP_DETAIL_TAB_URLS: string[] = [...VALID_TABS];

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
  toolsetSlug: string,
): string | undefined {
  if (!toolsetSlug) {
    return undefined;
  }

  const segments = pathname.split("/").filter(Boolean).map(decodePathSegment);
  const slugIndex = segments.findIndex(
    (segment, index) =>
      segment === toolsetSlug && segments[index - 1] === "mcp",
  );

  if (slugIndex === -1) {
    return undefined;
  }

  return segments[slugIndex + 1];
}

export function activeTabFromPath(
  pathname: string,
  toolsetSlug: string,
): TabValue | undefined {
  const tabSegment = tabSegmentFromPath(pathname, toolsetSlug);
  return tabSegment && isValidTab(tabSegment) ? tabSegment : undefined;
}

export function initialTabFromHash(
  hash: string,
  isRbacEnabled: boolean,
): TabValue {
  const hashValue = hash.replace("#", "");
  if (!isValidTab(hashValue)) return "overview";
  if (hashValue === "team-access" && !isRbacEnabled) return "overview";
  return hashValue;
}

export function mcpDetailTabHref(
  routes: ReturnType<typeof useRoutes>,
  toolsetSlug: string,
  tab: TabValue,
): string {
  switch (tab) {
    case "overview":
      return routes.mcp.details.overview.href(toolsetSlug);
    case "tools":
      return routes.mcp.details.tools.href(toolsetSlug);
    case "resources":
      return routes.mcp.details.resources.href(toolsetSlug);
    case "prompts":
      return routes.mcp.details.prompts.href(toolsetSlug);
    case "authentication":
      return routes.mcp.details.authentication.href(toolsetSlug);
    case "performance":
      return routes.mcp.details.performance.href(toolsetSlug);
    case "team-access":
      return routes.mcp.details.teamAccess.href(toolsetSlug);
    case "settings":
      return routes.mcp.details.settings.href(toolsetSlug);
  }
}
