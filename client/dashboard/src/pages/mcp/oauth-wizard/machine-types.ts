import type { RemoteSessionIssuerDraft } from "@gram/client/models/components/remotesessionissuerdraft.js";

type ExternalFormKey = "issuerUrl" | "metadataJson";

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
  initialPath?: "external";
  external: {
    issuerUrl: string;
    metadataJson: string;
    verifiedMetadata: RemoteSessionIssuerDraft | null;
    jsonError: string | null;
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
  };
  envSlug: string | null;
  error: string | null;
  result: { success: boolean; message: string } | null;
  toolsetId: string;
  toolsetSlug: string;
  toolsetName: string;
  activeOrganizationId: string;
};

export type Input = {
  initialPath?: "external";
  toolsetId: string;
  toolsetSlug: string;
  toolsetName: string;
  activeOrganizationId: string;
};

export type WizardEvent =
  | { type: "SELECT_EXTERNAL" }
  | { type: "SELECT_PROXY" }
  | { type: "SELECT_PROVIDER_ISSUER" }
  | { type: "SELECT_GRAM_HOSTED" }
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
