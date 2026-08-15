// The slug rides in the first path segment of `redirect`, which is all the
// server reads back as the organization: organizationSlugFromDestinationURL,
// impl.go:629. Anything after that segment is the dashboard's to route on.
function loginUrl(slug: string, destination: string): string | undefined {
  const appUrl = __GRAM_APP_URL__;
  if (!appUrl || !slug) {
    return undefined;
  }

  const url = new URL("/rpc/auth.login", appUrl);
  url.searchParams.set("redirect", `/${slug}${destination}`);

  return url.toString();
}

export function impersonationUrl(slug: string): string | undefined {
  return loginUrl(slug, "");
}

// Out of the admin app on purpose. Every `productfeatures` endpoint here reads
// and writes the operator's own organization, which is fixed at login and has
// no per-request override, so an in-app Features view would edit the wrong
// record in silence. AGE-3242 tracks the real one.
export function organizationFeaturesUrl(slug: string): string | undefined {
  return loginUrl(slug, "/platform-admin/features");
}
