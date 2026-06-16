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
// Falls back to client_secret_basic when the issuer advertises no recognized
// method, so DCR always sends one — upstreams that require an explicit method
// reject a registration that omits it ("No supported Token Endpoint Auth
// Method provided."). This fallback was the pre-#2910 server-side default.
export function pickPreferredAuthMethod(
  supported: string[],
): CreateRemoteSessionClientFormTokenEndpointAuthMethod {
  const { ClientSecretBasic, ClientSecretPost, None } =
    CreateRemoteSessionClientFormTokenEndpointAuthMethod;
  for (const preferred of [ClientSecretBasic, ClientSecretPost, None]) {
    if (supported.includes(preferred)) return preferred;
  }
  return ClientSecretBasic;
}

// Derive a unique slug from the Issuer URL's hostname. Mirrors the hyphen-style
// transform an operator would reasonably hand-write so the auto-filled value
// looks natural. Returns null for unparseable URLs — callers keep the prior slug
// in that case so partial typing doesn't blow it away.
export function deriveSlugFromUrl(url: string): string | null {
  const trimmed = url.trim();
  if (!trimmed) return null;
  try {
    const host = new URL(trimmed).hostname;
    if (!host) return null;
    const slug = host
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "");
    return slug || null;
  } catch {
    return null;
  }
}

// Derive a default Display name from the Issuer URL's hostname. Unlike the slug
// transform this keeps the hostname human-readable (no hyphenation/lowercasing).
// Returns null for unparseable URLs so callers leave the prior value intact while
// a partial URL is being typed.
export function deriveNameFromUrl(url: string): string | null {
  const trimmed = url.trim();
  if (!trimmed) return null;
  try {
    const host = new URL(trimmed).hostname;
    return host || null;
  } catch {
    return null;
  }
}
