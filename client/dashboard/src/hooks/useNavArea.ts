import { useLocation } from "react-router";

export type NavArea =
  | "Observability"
  | "MCP Gateway"
  | "Security and Policy"
  | "Organization";

// First path segment after /projects/:slug/ → sidebar area. Kept as a plain
// slug map (rather than deriving from useRoutes) so consumers stay light —
// routes.tsx imports every page component, which drags the whole app graph
// into any test that renders a component using this hook.
const AREA_BY_PAGE_SLUG: Record<string, NavArea> = {
  // Observability
  costs: "Observability",
  insights: "Observability",
  "agent-sessions": "Observability",
  "org-memory": "Observability",
  logs: "Observability",
  employees: "Observability",
  // Security and Policy
  watchdog: "Security and Policy",
  "risk-overview": "Security and Policy",
  "risk-policies": "Security and Policy",
  "risk-events": "Security and Policy",
  "shadow-mcp": "Security and Policy",
  "request-access": "Security and Policy",
  "detection-rules": "Security and Policy",
  // MCP Gateway
  mcp: "MCP Gateway",
  playground: "MCP Gateway",
  deployments: "MCP Gateway",
  skills: "MCP Gateway",
  plugins: "MCP Gateway",
  environments: "MCP Gateway",
  assistants: "MCP Gateway",
};

// Org-level pages that should not read as "Organization" administration.
const ORG_AREA_EXCLUDED = new Set(["login", "register", "explore-demo", "cli"]);

/**
 * The sidebar area the current page belongs to. Single source of truth shared
 * by the sidebar group highlight and the page-title eyebrow (Page.Eyebrow) so
 * the two never disagree. Project pages outside the four areas (home, chat)
 * return undefined; org-level pages return "Organization".
 */
export function useNavArea(): NavArea | undefined {
  const { pathname } = useLocation();

  // Anchored to the fixed /:orgSlug/projects/:projectSlug/:page shape so an
  // org or project literally slugged "projects" can't shift which segment is
  // read as the page.
  const projectPage = pathname.match(
    /^\/[^/]+\/projects\/[^/]+\/([^/?#]+)/,
  )?.[1];
  if (projectPage) return AREA_BY_PAGE_SLUG[projectPage];
  if (/^\/[^/]+\/projects(\/|$)/.test(pathname)) return undefined;

  const firstSegment = pathname.split("/").filter(Boolean)[0];
  if (!firstSegment || ORG_AREA_EXCLUDED.has(firstSegment)) return undefined;
  return "Organization";
}
