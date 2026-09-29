import { pickPreferredAuthMethod } from "@/pages/mcp/x/tabs/settings/sections/authentication/issuerFormUtils";
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

// The issuer form's method list is wider than what this RPC accepts: it also
// carries private_key_jwt, which the composite cannot express. In practice
// pickPreferredAuthMethod never returns it — it falls back to
// client_secret_basic when nothing recognized is advertised — so this narrows
// the type at the boundary rather than changing which method is sent.
export function serverIdentityAuthMethod(
  supported: string[],
): ServerIdentityClientConfigurationTokenEndpointAuthMethod {
  switch (pickPreferredAuthMethod(supported)) {
    case "client_secret_post":
      return "client_secret_post";
    case "none":
      return "none";
    case "client_secret_basic":
    // The composite has no private_key_jwt; pickPreferredAuthMethod cannot
    // return it either, so this only satisfies exhaustiveness.
    case "private_key_jwt":
      return "client_secret_basic";
  }
}
