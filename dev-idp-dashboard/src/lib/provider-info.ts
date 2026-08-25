import type { Mode } from "@/lib/devidp";

export interface ProviderInfo {
  capabilities: ReadonlyArray<string>;
  longDescription: string;
}

export const PROVIDER_INFO: Record<Mode, ProviderInfo> = {
  "oauth2-1": {
    capabilities: ["Local identity", "MCP OAuth Issuer"],
    longDescription:
      "The OAuth 2.1 authorization server backs both dashboard login and MCP auth. Under the local backend it signs you in as a row from dev-idp's own users table, seeded from your git committer identity. PKCE and dynamic client registration are supported and required of every client except the first-party login client.",
  },
  workos: {
    capabilities: ["Real WorkOS identity"],
    longDescription:
      "Under the workos backend, dev-idp resolves your identity through the live WorkOS API and signs you in as that user without a hosted login page. Set the subject here to log in as a different WorkOS user.",
  },
};
