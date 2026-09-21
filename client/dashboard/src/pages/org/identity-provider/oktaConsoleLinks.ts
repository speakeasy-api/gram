const OKTA_ORG_HOST_SUFFIXES = [
  "okta.com",
  "oktapreview.com",
  "okta-emea.com",
  "okta.mil",
] as const;

const HOST_LABEL = "[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?";
const PLAIN_HOSTNAME = new RegExp(`^${HOST_LABEL}(?:\\.${HOST_LABEL})+$`);

/** Mirrors the create API and okta.parseOrgURL: https, an ASCII plain hostname (no userinfo, port, path beyond one trailing slash, query, or fragment) that is a subdomain of an Okta-owned suffix. */
export function normalizeOktaOrgUrl(input: string): string | undefined {
  const match = /^https:\/\/([^/?#\s@:]+)\/?$/i.exec(input.trim());
  const rawHost = match?.[1];
  if (!rawHost || !PLAIN_HOSTNAME.test(rawHost)) return undefined;
  const host = rawHost.toLowerCase();
  const ok = OKTA_ORG_HOST_SUFFIXES.some((suffix) =>
    host.endsWith(`.${suffix}`),
  );
  return ok ? `https://${host}` : undefined;
}

/** The Okta admin console for an org URL (`acme.okta.com` → `acme-admin.okta.com`). */
export function oktaAdminConsoleUrl(orgUrl: string): string {
  const normalized = normalizeOktaOrgUrl(orgUrl);
  if (!normalized) return orgUrl;
  for (const suffix of OKTA_ORG_HOST_SUFFIXES) {
    if (normalized.endsWith(`.${suffix}`)) {
      const tenant = normalized
        .slice(0, -(suffix.length + 1))
        .replace(/-admin$/, "");
      return `${tenant}-admin.${suffix}`;
    }
  }
  return orgUrl;
}

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
