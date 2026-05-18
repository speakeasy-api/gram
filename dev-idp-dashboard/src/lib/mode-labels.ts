import type { Mode } from "@/lib/devidp";

export const MODE_LABELS: Record<Mode, string> = {
  "mock-workos": "mock-workos",
  "oauth2-1": "oauth2.1",
  oauth2: "oauth2.0",
  workos: "workos",
};

export const MODE_SUBTITLES: Record<Mode, string> = {
  "mock-workos":
    "Mock WorkOS REST surface — fully offline user/org/membership lookups.",
  "oauth2-1": "OAuth 2.1 AS — PKCE required, DCR, OIDC.",
  oauth2: "OAuth 2.0 AS — PKCE optional, no DCR, OIDC.",
  workos:
    "Live WorkOS REST proxy (mounted only when GRAM_IDP_CLIENT_SECRET is a real key).",
};

/** Sidebar grouping for the providers sub-nav. Order here is render order. */
export const MODE_GROUPS: ReadonlyArray<{
  title: string;
  modes: ReadonlyArray<Mode>;
}> = [
  { title: "WorkOS API", modes: ["mock-workos", "workos"] },
  { title: "OAuth Providers", modes: ["oauth2-1", "oauth2"] },
];
