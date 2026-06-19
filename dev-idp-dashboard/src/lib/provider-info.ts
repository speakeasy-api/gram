import type { Mode } from "@/lib/devidp";

export interface ProviderInfo {
  capabilities: ReadonlyArray<string>;
  longDescription: string;
}

export const PROVIDER_INFO: Record<Mode, ProviderInfo> = {
  "mock-workos": {
    capabilities: ["WorkOS Emulator"],
    longDescription:
      "Emulates the WorkOS REST surface (users, orgs, memberships, roles, invitations) backed by the dev-idp's own SQLite database. Use this for fully offline development where Gram resolves all WorkOS API calls locally.",
  },
  workos: {
    capabilities: ["WorkOS Proxy"],
    longDescription:
      "Proxies WorkOS REST calls to your real WorkOS dev environment. Use this when you want Gram exercised against actual WorkOS data — invitations, organization roles, real seats.",
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
export type ActivatableMode = Extract<Mode, "mock-workos" | "workos">;

export function isActivatable(mode: Mode): mode is ActivatableMode {
  return mode === "mock-workos" || mode === "workos";
}
