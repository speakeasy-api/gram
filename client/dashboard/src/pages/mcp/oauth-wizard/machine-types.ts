export type DiscoveredOAuth = {
  slug: string;
  name: string;
  version: string;
  metadata: Record<string, unknown>;
};

type ExternalFormKey = "slug" | "metadataJson";

export type ProxyFormKey =
  | "slug"
  | "authorizationEndpoint"
  | "tokenEndpoint"
  | "scopes"
  | "audience"
  | "tokenAuthMethod"
  | "environmentSlug"
  | "clientId"
  | "clientSecret";

export type Context = {
  discovered: DiscoveredOAuth | null;
  external: {
    slug: string;
    metadataJson: string;
    jsonError: string | null;
    prefilled: boolean;
  };
  proxy: {
    slug: string;
    authorizationEndpoint: string;
    tokenEndpoint: string;
    scopes: string;
    audience: string;
    tokenAuthMethod: string;
    environmentSlug: string;
    clientId: string;
    clientSecret: string;
    prefilled: boolean;
  };
  envSlug: string | null;
  error: string | null;
  // True when the user picked "Fetch Credentials" so submission is driven by
  // auto-registration. Lets the rendering layer keep the chooser/loading UI
  // visible during creatingEnvironment + creatingProxy instead of flashing
  // the manual credentials form.
  autoRegistering: boolean;
  result: { success: boolean; message: string } | null;
  toolsetSlug: string;
  toolsetName: string;
  activeOrganizationId: string;
};

export type Input = {
  discovered: DiscoveredOAuth | null;
  toolsetSlug: string;
  toolsetName: string;
  activeOrganizationId: string;
};

export type WizardEvent =
  | { type: "SELECT_EXTERNAL" }
  | { type: "SELECT_PROXY" }
  | { type: "SELECT_PROXY_AUTO" }
  | { type: "APPLY_DISCOVERED" }
  | { type: "FIELD_EXTERNAL"; key: ExternalFormKey; value: string }
  | { type: "FIELD_PROXY"; key: ProxyFormKey; value: string }
  | { type: "BACK" }
  | { type: "NEXT" }
  | { type: "SUBMIT" }
  | { type: "RESET" };

export function parseScopes(raw: string): string[] {
  return raw
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}

/** Token endpoint auth methods, ordered by our preference. */
export const TOKEN_AUTH_METHODS_PRIORITY = [
  "client_secret_basic",
  "client_secret_post",
  "none",
] as const;

// Picks the most preferred token endpoint auth method advertised by the issuer:
// client_secret_basic > client_secret_post > none. Falls back to
// client_secret_basic when the list is empty or holds no recognized method, so
// DCR always sends one — upstreams like Make reject a registration that omits
// it or sends an unsupported one ("No supported Token Endpoint Auth Method
// provided.").
export function pickAuthMethodFromList(supported: string[]): string {
  for (const method of TOKEN_AUTH_METHODS_PRIORITY) {
    if (supported.includes(method)) return method;
  }
  return "client_secret_basic";
}

// Origin (scheme://host) of the first parseable URL, or "" when none parse.
// Used as the RFC 8414 issuer for live auth-method discovery: discover's first
// probe candidate is `{origin}/.well-known/oauth-authorization-server`, where
// the Authorization Server metadata (and its token_endpoint_auth_methods_supported
// list) lives. Passing a path-bearing URL risks matching an OIDC document
// first, which omits the methods.
export function authServerOrigin(...candidates: string[]): string {
  for (const candidate of candidates) {
    const trimmed = candidate.trim();
    if (!trimmed) continue;
    try {
      return new URL(trimmed).origin;
    } catch {
      // Not a parseable absolute URL — try the next candidate.
    }
  }
  return "";
}
