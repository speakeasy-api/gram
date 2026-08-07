export const PREFERRED_THEME_STORAGE_KEY = "preferred-theme";
// Org-scoped list of favorited project UUIDs. Opaque ids only, so it is
// preserved across logout (shared-profile teammates share the list).
export const PROJECT_FAVORITES_STORAGE_PREFIX = "gram:org-favorites:";
// Command-palette "Recently Visited" pages. The key embeds the user id, but
// the values (labels, slugs) still reveal one user's navigation to the next
// person on a shared machine, so logout clears them; theme and favorites survive.
export const RECENTS_STORAGE_PREFIX = "gram:recents:";
