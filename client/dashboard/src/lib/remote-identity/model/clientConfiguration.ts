import type { ServerIdentityClientConfigurationTokenEndpointAuthMethod } from "@gram/client/models/components/serveridentityclientconfiguration.js";

/**
 * Every scope on offer: the protected resource's first, then the issuer's.
 *
 * Options for the manual scope picker only. An automatically created client
 * stores no scope: the server discovers what to request at each sign-in, so a
 * copied list cannot go stale or pose as an operator's choice.
 */
export function advertisedScopes(
  protectedResourceScopes: string[] | undefined | null,
  authorizationServerScopes: string[] | undefined | null,
): string[] {
  return normalizeScopes([
    ...(protectedResourceScopes ?? []),
    ...(authorizationServerScopes ?? []),
  ]);
}

/** One scope per entry, without repeats: a pasted "read write" is two. */
export function normalizeScopes(values: string[]): string[] {
  return [
    ...new Set(nonEmptyStrings(values.flatMap((value) => value.split(/\s+/)))),
  ];
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

type PreferredAuthMethod = (typeof PREFERRED_AUTH_METHODS)[number];

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
