import { useLocation } from "react-router";

export type NavArea =
  | "Observe"
  | "Secure"
  | "Connect"
  | "Distribute"
  | "Organization";

// First path segment after /projects/:slug/ → sidebar area. Kept as a plain
// slug map (rather than deriving from useRoutes) so consumers stay light —
// routes.tsx imports every page component, which drags the whole app graph
// into any test that renders a component using this hook.
const AREA_BY_PAGE_SLUG: Record<string, NavArea> = {
  // Observe
  costs: "Observe",
  insights: "Observe",
  explore: "Observe",
  "agent-sessions": "Observe",
  "org-memory": "Observe",
  logs: "Observe",
  employees: "Observe",
  // Secure
  watchdog: "Secure",
  "risk-overview": "Secure",
  "risk-policies": "Secure",
  "risk-events": "Secure",
  "shadow-mcp": "Secure",
  "request-access": "Secure",
  "detection-rules": "Secure",
  // Connect
  sources: "Connect",
  catalog: "Connect",
  playground: "Connect",
  deployments: "Connect",
  // Distribute
  mcp: "Distribute",
  skills: "Distribute",
  plugins: "Distribute",
  environments: "Distribute",
  assistants: "Distribute",
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
