// The slug rides in `redirect`, whose first path segment the server reads back
// as the organization: organizationSlugFromDestinationURL, impl.go:629.
export function impersonationUrl(slug: string): string | undefined {
  const appUrl = __GRAM_APP_URL__;
  if (!appUrl || !slug) {
    return undefined;
  }

  const url = new URL("/rpc/auth.login", appUrl);
  url.searchParams.set("redirect", `/${slug}`);

  return url.toString();
}
