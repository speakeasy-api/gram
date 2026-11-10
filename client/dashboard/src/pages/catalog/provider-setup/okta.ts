export const oktaGuide =
  "https://help.okta.com/mcp/en-us/content/topics/mcpserver/oidc-pkce-browser-based.htm";
export const oktaScopeGuide =
  "https://help.okta.com/mcp/en-us/content/topics/mcpserver/scope-based-tool-loading.htm";
export const oktaRemoteTemplate = "https://{yourOktaDomain}/mcp";
export const oktaSpecifier = "com.okta/managed-mcp";

/** Public Okta org domains supported by this guided Managed MCP connection. */
export function oktaOrg(value: string): string | null {
  try {
    const url = new URL(
      value.includes("://") ? value.trim() : `https://${value.trim()}`,
    );
    if (
      url.protocol !== "https:" ||
      url.port ||
      url.username ||
      url.password ||
      url.search ||
      url.hash ||
      url.pathname !== "/"
    )
      return null;
    if (
      !/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.(?:okta\.com|oktapreview\.com|okta-emea\.com)$/.test(
        url.hostname,
      )
    )
      return null;
    return url.origin;
  } catch {
    return null;
  }
}
export function oktaSelectionForUrl(
  value: string | undefined,
): { oktaOrg: string } | null {
  try {
    const url = new URL(value ?? "");
    const org = oktaOrg(url.origin);
    if (
      !org ||
      url.username ||
      url.password ||
      url.search ||
      url.hash ||
      url.pathname !== "/mcp"
    )
      return null;
    return { oktaOrg: org };
  } catch {
    return null;
  }
}
export type OktaConnection = {
  name: string;
  url: string;
  issuer: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  authMethod: "none" | "client_secret_basic";
};
export function oktaConnection(values: {
  oktaOrg: string;
  oktaAppType: string;
}): OktaConnection | null {
  const org = oktaOrg(values.oktaOrg);
  if (!org || !["web", "native"].includes(values.oktaAppType)) return null;
  return {
    name: "Okta",
    url: `${org}/mcp`,
    issuer: org,
    authorizationEndpoint: `${org}/oauth2/v1/authorize`,
    tokenEndpoint: `${org}/oauth2/v1/token`,
    authMethod:
      values.oktaAppType === "native"
        ? ("none" as const)
        : ("client_secret_basic" as const),
  };
}
/** No scopes are added implicitly, including refresh-token or OIDC scopes. */
export function oktaScopes(value: string): string[] | null {
  const scopes = [
    ...new Set(
      value
        .trim()
        .split(/[\s,]+/)
        .filter(Boolean),
    ),
  ];
  if (
    !scopes.length ||
    scopes.length > 100 ||
    !scopes.some((s) => s.startsWith("okta."))
  )
    return null;
  if (
    !scopes.every((s) =>
      /^(?:okta\.[A-Za-z][A-Za-z0-9]*(?:\.[A-Za-z][A-Za-z0-9]*)+|openid|profile|email|offline_access)$/.test(
        s,
      ),
    )
  )
    return null;
  return scopes;
}
