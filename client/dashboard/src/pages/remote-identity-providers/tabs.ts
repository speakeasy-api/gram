// Tab sets for the Remote Identity Provider and Remote Session Client detail
// pages. The resolver that turns a pathname into one of these lives in
// @/lib/detail-tabs.

export const ISSUER_TABS = ["overview", "clients", "settings"] as const;
export type IssuerTab = (typeof ISSUER_TABS)[number];

export const CLIENT_TABS = [
  "overview",
  "mcp-servers",
  "sessions",
  "settings",
] as const;
export type ClientTab = (typeof CLIENT_TABS)[number];
