// Tab set for the signing key set detail page. The resolver that turns a
// pathname into one of these lives in @/lib/detail-tabs.

export const JSON_WEB_KEY_SET_TABS = ["overview", "keys", "settings"] as const;
export type JsonWebKeySetTab = (typeof JSON_WEB_KEY_SET_TABS)[number];
