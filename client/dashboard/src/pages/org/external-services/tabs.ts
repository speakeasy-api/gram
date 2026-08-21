// Tab set for the external credential detail page. The resolver that turns a
// pathname into one of these lives in @/lib/detail-tabs.

export const EXTERNAL_CREDENTIAL_TABS = [
  "overview",
  "kms-keys",
  "settings",
] as const;
export type ExternalCredentialTab = (typeof EXTERNAL_CREDENTIAL_TABS)[number];
