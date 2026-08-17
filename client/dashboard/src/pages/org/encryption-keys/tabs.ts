// Tab set for the encryption key detail page. The resolver that turns a
// pathname into one of these lives in @/lib/detail-tabs.

export const EXTERNAL_KEY_TABS = ["overview", "settings"] as const;
export type ExternalKeyTab = (typeof EXTERNAL_KEY_TABS)[number];
