// The shared read-only demo org. Sessions enter it via /explore-demo
// (auth.enterDemo) or the platform-admin override cookie; identity is carried
// by the org slug on the frontend.
export const DEMO_ORG_SLUG = "acme-demo";

// The demo org is seeded with a single project. New visitors land here so
// they see sample data instead of the empty org home.
const DEMO_PROJECT_SLUG = "default";
const DEMO_APP_ORIGIN = "https://app.getgram.ai";

export const DEMO_LANDING_PATH = `/${DEMO_ORG_SLUG}/projects/${DEMO_PROJECT_SLUG}`;

/**
 * Query-parameter key used by ExploreDemo to land the visitor on a specific
 * page after switching their session into the demo org.
 */
export const DEMO_REDIRECT_PARAM = "redirect";

/**
 * Link to the demo-org equivalent of a page. Routes through /explore-demo so
 * auth.enterDemo runs first — a direct deep link would fail for users whose
 * session is scoped to a different org.
 */
export function demoProjectPageHref(
  pathname: string,
  projectSlug?: string,
): string {
  const projectRoot = projectSlug ? `/projects/${projectSlug}` : undefined;
  const projectRootIndex = projectRoot ? pathname.indexOf(projectRoot) : -1;
  const pagePath =
    projectRootIndex >= 0
      ? pathname.slice(projectRootIndex + (projectRoot?.length ?? 0))
      : "";

  const demoPath = `${DEMO_LANDING_PATH}${pagePath}`;
  return `${DEMO_APP_ORIGIN}/explore-demo?${DEMO_REDIRECT_PARAM}=${encodeURIComponent(demoPath)}`;
}

// Set by the /explore-demo page before switching, so Exit demo can return a
// multi-org user to the org they actually came from.
export const PRE_DEMO_ORG_KEY = "gram:pre-demo-org";
