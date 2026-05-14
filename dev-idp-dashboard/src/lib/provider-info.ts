import type { Mode } from "@/lib/devidp";

export interface ProviderInfo {
  capabilities: ReadonlyArray<string>;
  longDescription: string;
}

export const PROVIDER_INFO: Record<Mode, ProviderInfo> = {
  "local-speakeasy": {
    capabilities: ["Gram Login Provider", "WorkOS Emulator"],
    longDescription:
      "Stands in for the Speakeasy provider that backs Gram management-API login, and emulates the WorkOS REST surface so user/org/membership lookups resolve locally without an external dependency. Use this when you want a fully offline auth setup keyed off the dev-idp's own database.",
  },
  workos: {
    capabilities: ["Gram Login Provider", "WorkOS Emulator"],
    longDescription:
      "Speakeasy login still flows through the dev-idp, but user / org / membership lookups proxy to your real WorkOS environment. Use this when you want Gram exercised against actual WorkOS data — invitations, organization roles, real seats.",
  },
  "oauth2-1": {
    capabilities: ["MCP OAuth Issuer"],
    longDescription:
      "OAuth 2.1 authorization server with PKCE required, dynamic client registration, and OIDC. Use this when Gram is acting as an MCP OAuth issuer for clients that follow the modern OAuth profile.",
  },
  oauth2: {
    capabilities: ["MCP OAuth Issuer"],
    longDescription:
      "OAuth 2.0 authorization server with optional PKCE, no DCR, OIDC. Use this to test MCP clients that follow the older OAuth profile.",
  },
};

/** Modes that the dashboard can flip Gram between via env-var rewrites. */
export type ActivatableMode = Extract<Mode, "local-speakeasy" | "workos">;

export function isActivatable(mode: Mode): mode is ActivatableMode {
  return mode === "local-speakeasy" || mode === "workos";
}
