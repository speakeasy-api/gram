export type DiscoveredOAuth = {
  slug: string;
  name: string;
  version: string;
  metadata: Record<string, unknown>;
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
  | { type: "APPLY_DISCOVERED" }
  | { type: "FIELD_EXTERNAL"; key: ExternalFormKey; value: string }
  | { type: "FIELD_PROXY"; key: ProxyFormKey; value: string }
  | { type: "BACK" }
  | { type: "NEXT" }
  | { type: "SUBMIT" }
  | { type: "AUTO_REGISTER" }
  | { type: "MANUAL_CREDENTIALS" }
  | { type: "RESET" };

export function parseScopes(raw: string): string[] {
  return raw
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}
