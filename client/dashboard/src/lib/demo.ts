// The shared read-only demo org. Sessions enter it via /explore-demo
// (auth.enterDemo) or the platform-admin override cookie; identity is carried
// by the org slug on the frontend.
export const DEMO_ORG_SLUG = "acme-demo";

// The demo org is seeded with a single project. New visitors land here so
// they see sample data instead of the empty org home.
const DEMO_PROJECT_SLUG = "default";

export const DEMO_LANDING_PATH = `/${DEMO_ORG_SLUG}/projects/${DEMO_PROJECT_SLUG}`;

/**
 * Query-parameter key used by ExploreDemo to land the visitor on a specific
 * page after switching their session into the demo org.
 */
export const DEMO_REDIRECT_PARAM = "redirect";

// Set by the /explore-demo page before switching, so Exit demo can return a
// multi-org user to the org they actually came from.
export const PRE_DEMO_ORG_KEY = "gram:pre-demo-org";
