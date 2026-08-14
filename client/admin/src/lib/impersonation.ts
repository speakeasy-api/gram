// Builds the link that drops an operator into the customer-facing dashboard
// with a chosen organization active. There is no impersonation endpoint and no
// cookie: the whole mechanism is the login endpoint's `redirect` parameter.
//
// The server decodes `redirect` into the login state, then reads the
// organization slug back out of its first path segment
// (organizationSlugFromDestinationURL, server/internal/auth/impl.go:629-647)
// and, for an admin account, resolves that slug straight from the database
// even when the operator is not a member (impl.go:411-418). A non-admin
// account gets no error, just their own default organization.
export function impersonationUrl(slug: string): string | undefined {
  // Empty at build time when GRAM_APP_URL is unset. Callers drop the action
  // rather than render a link that goes nowhere.
  const appUrl = __GRAM_APP_URL__;
  if (!appUrl || !slug) {
    return undefined;
  }

  const url = new URL("/rpc/auth.login", appUrl);
  // Relative on purpose. The server refuses to redirect off its own origin, so
  // an absolute destination would be stripped back to this anyway.
  url.searchParams.set("redirect", `/${slug}`);

  return url.toString();
}
