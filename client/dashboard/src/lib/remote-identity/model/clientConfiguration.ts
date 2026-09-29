import type { ServerIdentityClientConfigurationTokenEndpointAuthMethod } from "@gram/client/models/components/serveridentityclientconfiguration.js";

/**
 * The scopes a new client should request.
 *
 * The protected resource's RFC 9728 `scopes_supported` wins because it names
 * what this one server needs. The issuer's list is a fallback only: it names
 * everything the provider can grant, and requesting all of it is what broke
 * Salesforce logins. Every surface that registers a client must go through
 * here so a client does not come out different depending on who created it.
 */
export function preferredScopes(
  protectedResourceScopes: string[] | undefined | null,
  authorizationServerScopes: string[] | undefined | null,
): string[] {
  const resourceScopes = nonEmptyStrings(protectedResourceScopes);
  return resourceScopes.length > 0
    ? resourceScopes
    : nonEmptyStrings(authorizationServerScopes);
}

function nonEmptyStrings(values: string[] | undefined | null): string[] {
  return (values ?? [])
    .map((value) => value.trim())
    .filter((value) => value.length > 0);
}

// Token endpoint authentication methods Speakeasy can use for a client it
// registers, most preferred first.
const PREFERRED_AUTH_METHODS = [
  "client_secret_basic",
  "client_secret_post",
  "none",
] as const;

export type PreferredAuthMethod = (typeof PREFERRED_AUTH_METHODS)[number];

/** The most preferred method the issuer advertises, or undefined for none. */
function firstPreferredAuthMethod(
  supported: readonly string[],
): PreferredAuthMethod | undefined {
  return PREFERRED_AUTH_METHODS.find((method) => supported.includes(method));
}

/**
 * The method to pick in the issuer form from the issuer's advertised list.
 * Preference order: client_secret_basic > client_secret_post > none.
 *
 * Falls back to client_secret_basic when the issuer advertises no recognized
 * method, so DCR always sends one — upstreams that require an explicit method
 * reject a registration that omits it ("No supported Token Endpoint Auth
 * Method provided."). This fallback was the pre-#2910 server-side default.
 */
export function pickPreferredAuthMethod(
  supported: readonly string[],
): PreferredAuthMethod {
  return firstPreferredAuthMethod(supported) ?? "client_secret_basic";
}

/**
 * The method to register a server identity client with.
 *
 * An issuer that advertises nothing gets the RFC 8414 default,
 * client_secret_basic. One that advertises only methods Speakeasy cannot use
 * (private_key_jwt, say) gets undefined: naming client_secret_basic there
 * would register a client the issuer refuses, so the field is omitted and the
 * server decides.
 */
export function serverIdentityAuthMethod(
  supported: readonly string[],
): ServerIdentityClientConfigurationTokenEndpointAuthMethod | undefined {
  if (supported.length === 0) return "client_secret_basic";
  return firstPreferredAuthMethod(supported);
}
