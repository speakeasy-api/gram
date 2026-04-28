export type DiscoveredOAuth = {
  slug: string;
  name: string;
  version: string;
  metadata: Record<string, unknown>;
};

export type ProxyDefaults = {
  slug: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  scopes: string;
  audience: string;
  tokenAuthMethod: string;
  environmentSlug: string;
};

export type ExternalFormKey = "slug" | "metadataJson";

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
  mode: "create" | "edit";
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
    audiencePrefilled: string;
    tokenAuthMethod: string;
    environmentSlug: string;
    clientId: string;
    clientSecret: string;
    prefilled: boolean;
  };
  envSlug: string | null;
  error: string | null;
  result: { success: boolean; message: string } | null;
  toolsetSlug: string;
  toolsetName: string;
  activeOrganizationId: string;
  existingEnvNames: string[];
};

export type Input = {
  mode: "create" | "edit";
  discovered: DiscoveredOAuth | null;
  toolsetSlug: string;
  toolsetName: string;
  activeOrganizationId: string;
  existingEnvNames: string[];
  editProxyDefaults: ProxyDefaults | null;
};

export type WizardEvent =
  | { type: "SELECT_EXTERNAL" }
  | { type: "SELECT_PROXY" }
  | { type: "APPLY_DISCOVERED" }
  | { type: "FIELD_EXTERNAL"; key: ExternalFormKey; value: string }
  | { type: "FIELD_PROXY"; key: ProxyFormKey; value: string }
  | { type: "BACK" }
  | { type: "NEXT" }
  | { type: "SUBMIT" }
  | { type: "SUBMIT_EDIT" }
  | { type: "RESET" };

export function parseScopes(raw: string): string[] {
  return raw
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}

export function audienceDirty(ctx: Context): boolean {
  return ctx.proxy.audience !== ctx.proxy.audiencePrefilled;
}
