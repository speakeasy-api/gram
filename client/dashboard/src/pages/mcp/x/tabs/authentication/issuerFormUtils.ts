import { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components";

// Snapshot of the issuer + RFC 8414 metadata for a given Issuer URL. Created
// fresh on every successful discovery and seeded from saved records in the
// Modify sheet. Drives the Discover/Reset slot and the URL-change reset.
export type DiscoveredEndpoints = {
  url: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  registrationEndpoint: string;
  jwksUri: string;
  scopesSupported: string[];
  grantTypesSupported: string[];
  responseTypesSupported: string[];
  tokenEndpointAuthMethodsSupported: string[];
};

// Matches the OAuth Proxy wizard's parseScopes helper: split on commas, trim,
// drop empties. Kept local rather than imported across feature folders so the
// authentication module stays self-contained.
export function parseScopes(raw: string): string[] {
  return raw
    .split(",")
    .map((scope) => scope.trim())
    .filter((scope) => scope.length > 0);
}

export function narrowTokenEndpointAuthMethod(
  value: string | null | undefined,
): CreateRemoteSessionClientFormTokenEndpointAuthMethod | undefined {
  if (
    value ===
      CreateRemoteSessionClientFormTokenEndpointAuthMethod.ClientSecretBasic ||
    value ===
      CreateRemoteSessionClientFormTokenEndpointAuthMethod.ClientSecretPost ||
    value === CreateRemoteSessionClientFormTokenEndpointAuthMethod.None
  ) {
    return value;
  }
  return undefined;
}

// Picks the preferred auth method from the issuer's advertised list.
// Preference order: client_secret_basic > client_secret_post > none.
// Returns undefined when none of the preferred methods are advertised.
export function pickPreferredAuthMethod(
  supported: string[],
): CreateRemoteSessionClientFormTokenEndpointAuthMethod | undefined {
  const { ClientSecretBasic, ClientSecretPost, None } =
    CreateRemoteSessionClientFormTokenEndpointAuthMethod;
  for (const preferred of [ClientSecretBasic, ClientSecretPost, None]) {
    if (supported.includes(preferred)) return preferred;
  }
  return undefined;
}
