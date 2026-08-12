// The shared read-only demo org. Sessions enter it via /explore-demo
// (auth.enterDemo) or the platform-admin override cookie; identity is carried
// by the org slug on the frontend.
export const DEMO_ORG_SLUG = "acme-demo";

// Set by the /explore-demo page before switching, so Exit demo can return a
// multi-org user to the org they actually came from.
export const PRE_DEMO_ORG_KEY = "gram:pre-demo-org";
