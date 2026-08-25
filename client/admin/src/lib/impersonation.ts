// Appended to the visible words of every link built here, in an `sr-only` span.
// One wording, because there is more than one such link and a screen reader
// hears the same fact about each: a second phrasing invented at a call site
// reads as two different things happening.
//
// The leading space belongs to it. Without one the two run together into a
// single word when they are read out.
export const LEAVES_THE_APP = " (opens in the Gram dashboard)";

// The slug rides in the first path segment of `redirect`, which is all the
// server reads back as the organization: organizationSlugFromDestinationURL,
// impl.go:629. Anything after that segment is the dashboard's to route on.
function loginUrl(slug: string, destination: string): string | undefined {
  const appUrl = __GRAM_APP_URL__;
  if (!appUrl || !slug) {
    return undefined;
  }

  const url = new URL("/rpc/auth.login", appUrl);
  // The slug and not the destination. An ordinary slug is unchanged by this;
  // a slug beginning with a slash would make the redirect `//host`, which is
  // protocol-relative and takes the operator off the origin. No slug validation
  // was found on the server side to rule that out. The destination is written
  // here and carries the slashes the dashboard routes on.
  url.searchParams.set(
    "redirect",
    `/${encodeURIComponent(slug)}${destination}`,
  );

  return url.toString();
}

// Out of the admin app on purpose. Every `productfeatures` endpoint here reads
// and writes the operator's own organization, which is fixed at login and has
// no per-request override, so an in-app Features view would edit the wrong
// record in silence. AGE-3242 tracks the real one.
export function organizationFeaturesUrl(slug: string): string | undefined {
  return loginUrl(slug, "/platform-admin/features");
}
