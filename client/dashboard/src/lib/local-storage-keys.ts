export const PREFERRED_THEME_STORAGE_KEY = "preferred-theme";
export const PROJECT_FAVORITES_STORAGE_PREFIX = "gram:org-favorites:";
// Command-palette "Recently Visited" pages. User-scoped, so safe to keep across
// logout (a different user reads their own key); preserved like favorites.
export const RECENTS_STORAGE_PREFIX = "gram:recents:";
// Maps an assistant to the chat id of its onboarding "setup" thread so
// reopening the assistant's setup restores the prior conversation. The setup
// chat is a plain project chat (not linked to the assistant server-side), so
// there's no server query for it — the mapping lives here. Not preserved across
// logout: it's project/assistant data that shouldn't leak to another user.
export const ASSISTANT_SETUP_THREAD_STORAGE_PREFIX =
  "gram:assistant-setup-thread:";
