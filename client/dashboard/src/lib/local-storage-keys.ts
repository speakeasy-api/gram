export const PREFERRED_THEME_STORAGE_KEY = "preferred-theme";
export const PROJECT_FAVORITES_STORAGE_PREFIX = "gram:org-favorites:";
// Command-palette "Recently Visited" pages. The key embeds the user id, but
// the values still reveal one user's navigation to the next person on a shared
// machine, so logout clears them along with favorites; only the theme survives.
export const RECENTS_STORAGE_PREFIX = "gram:recents:";
