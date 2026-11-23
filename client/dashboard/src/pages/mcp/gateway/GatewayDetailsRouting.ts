import { createDetailRouting } from "../detailRouting";

const routing = createDetailRouting({
  // "members" is not a tab: member management lives on the Overview tab, and
  // the legacy /members URL falls through the unknown-tab redirect to overview.
  tabs: [
    "overview",
    "inspect",
    "skills",
    "team-access",
    "sessions",
    "settings",
  ],
  idSegmentIndex: (segments, gatewayId) =>
    segments.findIndex(
      (segment, index) =>
        segment === gatewayId &&
        segments[index - 1] === "gateway" &&
        segments[index - 2] === "mcp",
    ),
  tabRoutes: (routes) => ({
    overview: routes.mcp.gateway.overview,
    inspect: routes.mcp.gateway.inspect,
    skills: routes.mcp.gateway.skills,
    "team-access": routes.mcp.gateway.teamAccess,
    sessions: routes.mcp.gateway.sessions,
    settings: routes.mcp.gateway.settings,
  }),
});

export const GATEWAY_TAB_URLS: string[] = routing.tabUrls;

export type TabValue = Parameters<typeof routing.tabHref>[2];

export const activeTabFromPath = routing.activeTabFromPath;

export function initialTabFromHash(hash: string): TabValue {
  const hashValue = hash.replace("#", "");
  if (!routing.isValidTab(hashValue)) return "overview";
  return hashValue;
}

export const gatewayTabHref = routing.tabHref;
