import { normalizeOktaOrgUrl, oktaAdminConsoleUrl } from "./connectionView";

/** Only link to HTTPS consoles on the supported Okta tenant domains. */
export function oktaConsoleUrl(
  deepLink: string | undefined,
): string | undefined {
  if (!deepLink) return undefined;
  try {
    const url = new URL(deepLink);
    if (
      url.protocol !== "https:" ||
      !normalizeOktaOrgUrl(url.origin) ||
      url.username ||
      url.password
    )
      return undefined;
    return url.toString();
  } catch {
    return undefined;
  }
}

/** Start at the agent's connection list, not the duplicate-prone create form. */
export function oktaConnectionsUrl(
  deepLink: string | undefined,
): string | undefined {
  const validated = oktaConsoleUrl(deepLink);
  if (!validated) return undefined;
  const url = new URL(validated);
  url.pathname = url.pathname.replace(
    /\/resource-connections\/create\/?$/,
    "/resource-connections",
  );
  return url.toString();
}

/** Applications is where Okta exposes a resource app's XAA Issuer URL. */
export function oktaApplicationsUrl(
  orgUrl: string | undefined,
): string | undefined {
  if (!orgUrl) return undefined;
  const normalized = normalizeOktaOrgUrl(orgUrl);
  if (!normalized) return undefined;
  return new URL(
    "/admin/apps/active",
    oktaAdminConsoleUrl(normalized),
  ).toString();
}
