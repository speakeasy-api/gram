import type { Mode } from "@/lib/devidp";

export const MODE_LABELS: Record<Mode, string> = {
  "local-speakeasy": "local-speakeasy",
  "oauth2-1": "oauth2.1",
  oauth2: "oauth2.0",
  workos: "workos",
};

export const MODE_SUBTITLES: Record<Mode, string> = {
  "local-speakeasy":
    "Speakeasy provider exchange — backs Gram management-API login.",
  "oauth2-1": "OAuth 2.1 AS — PKCE required, DCR, OIDC.",
  oauth2: "OAuth 2.0 AS — PKCE optional, no DCR, OIDC.",
  workos: "Live WorkOS REST proxy (mounted only when WORKOS_API_KEY is set).",
};

/** Sidebar grouping for the providers sub-nav. Order here is render order. */
export const MODE_GROUPS: ReadonlyArray<{
  title: string;
  modes: ReadonlyArray<Mode>;
}> = [
  { title: "Gram Login Providers", modes: ["local-speakeasy", "workos"] },
  { title: "OAuth Providers", modes: ["oauth2-1", "oauth2"] },
];
