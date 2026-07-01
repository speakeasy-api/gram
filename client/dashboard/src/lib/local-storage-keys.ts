export const PREFERRED_THEME_STORAGE_KEY = "preferred-theme";
export const PROJECT_FAVORITES_STORAGE_PREFIX = "gram:org-favorites:";
// Command-palette "Recently Visited" pages. User-scoped, so safe to keep across
// logout (a different user reads their own key); preserved like favorites.
export const RECENTS_STORAGE_PREFIX = "gram:recents:";
// Pointer to the chat thread backing an assistant's setup conversation, so
// reopening the assistant resumes the conversation. User-scoped (the key
// embeds project + user + assistant ids), so preserved across logout like
// recents — the transcript itself lives server-side.
export const ASSISTANT_SETUP_THREAD_STORAGE_PREFIX =
  "gram:assistant-setup-thread:";
