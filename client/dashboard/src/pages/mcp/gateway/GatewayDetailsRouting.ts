import type { useRoutes } from "@/routes";

const VALID_TABS = [
  "overview",
  "members",
  "inspect",
  "team-access",
  "sessions",
  "settings",
] as const;

// Breadcrumb `skipSegments` source. Derived from VALID_TABS rather than
// hand-maintained alongside it, so adding a tab can't leave a stray crumb.
export const GATEWAY_TAB_URLS: string[] = [...VALID_TABS];

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
  gatewayId: string,
): string | undefined {
  if (!gatewayId) {
    return undefined;
  }

  const segments = pathname.split("/").filter(Boolean).map(decodePathSegment);
  const gatewayIdIndex = segments.findIndex(
    (segment, index) =>
      segment === gatewayId &&
      segments[index - 1] === "gateway" &&
      segments[index - 2] === "mcp",
  );

  if (gatewayIdIndex === -1) {
    return undefined;
  }

  return segments[gatewayIdIndex + 1];
}

export function activeTabFromPath(
  pathname: string,
  gatewayId: string,
): TabValue | undefined {
  const tabSegment = tabSegmentFromPath(pathname, gatewayId);
  return tabSegment && isValidTab(tabSegment) ? tabSegment : undefined;
}

export function initialTabFromHash(hash: string): TabValue {
  const hashValue = hash.replace("#", "");
  if (!isValidTab(hashValue)) return "overview";
  return hashValue;
}

export function gatewayTabHref(
  routes: ReturnType<typeof useRoutes>,
  gatewayId: string,
  tab: TabValue,
): string {
  switch (tab) {
    case "overview":
      return routes.mcp.gateway.overview.href(gatewayId);
    case "members":
      return routes.mcp.gateway.members.href(gatewayId);
    case "inspect":
      return routes.mcp.gateway.inspect.href(gatewayId);
    case "team-access":
      return routes.mcp.gateway.teamAccess.href(gatewayId);
    case "sessions":
      return routes.mcp.gateway.sessions.href(gatewayId);
    case "settings":
      return routes.mcp.gateway.settings.href(gatewayId);
  }
}
