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
  // OAuth CIMD draft capability parsed from the discovery document: whether the
  // issuer accepts a Client ID Metadata Document URL as client_id. Persisted on
  // create/update so the CIMD client type can be offered for this issuer.
  clientIdMetadataDocumentSupported: boolean;
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

// ClientType selects how a new remote_session_client is created. It is a
// UI-only choice that routes to the matching create endpoint: DCR proxies a
// registration to obtain credentials, Manual takes caller-supplied
// credentials, and CIMD (Client ID Metadata Document) creates a
// credential-less client whose client_id is a Gram-hosted document URL.
export type ClientType = "dcr" | "manual" | "cimd";

export const CLIENT_TYPE_LABELS: Record<ClientType, string> = {
  dcr: "Dynamic Client Registration (DCR)",
  cimd: "Client ID Metadata Document (CIMD)",
  manual: "Manual",
};

// The selectable client types for an issuer. DCR and CIMD appear only when the
// issuer supports them; Manual is always available. The first entry is the
// default — an automatic type (DCR, then CIMD) when one is available, so the
// recommended path stays pre-selected while remaining switchable.
export function availableClientTypes({
  dcrAvailable,
  cimdAvailable,
}: {
  dcrAvailable: boolean;
  cimdAvailable: boolean;
}): ClientType[] {
  const types: ClientType[] = [];
  if (dcrAvailable) types.push("dcr");
  if (cimdAvailable) types.push("cimd");
  types.push("manual");
  return types;
}

// Help text shown under the Client Type selector. It explains the active type
// and, in Manual mode, points at DCR when the issuer supports it.
export function clientTypeHelp(
  clientType: ClientType,
  available: ClientType[],
): string {
  switch (clientType) {
    case "dcr":
      return "The issuer advertises a registration endpoint (RFC 7591), so the platform can automatically register a client on save. You can also choose to manually define an existing client.";
    case "cimd":
      return "The issuer supports Client ID Metadata Documents, so the platform hosts a public document and uses its URL as the client_id. No credentials are stored; the issuer authenticates the client by dereferencing that URL.";
    case "manual": {
      let help =
        "The platform acts as an OAuth client against the upstream issuer. Register a client with the issuer out-of-band and paste the credentials here.";
      if (available.includes("dcr")) {
        help +=
          " You can also choose Dynamic Client Registration (DCR) to register a client automatically.";
      }
      return help;
    }
  }
}
