// The shared read-only demo org. Sessions enter it via /explore-demo
// (auth.enterDemo) or the platform-admin override cookie; identity is carried
// by the org slug on the frontend.
export const DEMO_ORG_SLUG = "acme-demo";

// The demo org is seeded with a single project. New visitors land here so
// they see sample data instead of the empty org home.
const DEMO_PROJECT_SLUG = "default";
const DEMO_APP_ORIGIN = "https://app.getgram.ai";

export const DEMO_LANDING_PATH = `/${DEMO_ORG_SLUG}/projects/${DEMO_PROJECT_SLUG}`;

/** Link to the demo-org equivalent of a project page. */
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

  return `${DEMO_APP_ORIGIN}${DEMO_LANDING_PATH}${pagePath}`;
}

// Set by the /explore-demo page before switching, so Exit demo can return a
// multi-org user to the org they actually came from.
export const PRE_DEMO_ORG_KEY = "gram:pre-demo-org";
